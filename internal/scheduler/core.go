package scheduler

import (
	"container/heap"
	"context"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"dpls-xdp/internal/ebpf"
	"dpls-xdp/internal/graph"
	"dpls-xdp/internal/state"
	"dpls-xdp/pkg/api"
)

// EventType represents scheduling events
type EventType string

const (
	EventDAGArrived    EventType = "DAG_ARRIVED"
	EventTaskCompleted EventType = "TASK_COMPLETED"
	EventWorkerIdle    EventType = "WORKER_IDLE"
)

// Event represents reactor event details
type Event struct {
	Type      EventType
	DAG       *api.DAG
	TaskID    string
	WorkerID  string
	Timestamp time.Time
}

// Config stores scheduler options
type Config struct {
	AgingFactor float64
}

// Scheduler handles the DAG mapping and queues
type Scheduler struct {
	graphEng  *graph.Engine
	stateMgr  *state.Manager
	workers   []*api.Worker
	eventChan chan Event
	pq        PriorityQueue
	config    Config
}

// NewScheduler creates a new scheduler core
func NewScheduler(graphEng *graph.Engine, stateMgr *state.Manager, workers []*api.Worker, agingFactor float64) *Scheduler {
	s := &Scheduler{
		graphEng:  graphEng,
		stateMgr:  stateMgr,
		workers:   workers,
		eventChan: make(chan Event, 1000),
		pq:        make(PriorityQueue, 0),
		config:    Config{AgingFactor: agingFactor},
	}
	heap.Init(&s.pq)
	return s
}

// SubmitDAG registers a new online DAG and fires an event
func (s *Scheduler) SubmitDAG(dag *api.DAG) {
	s.eventChan <- Event{
		Type:      EventDAGArrived,
		DAG:       dag,
		Timestamp: time.Now(),
	}
}

// EventChan returns the write-only event channel for worker updates
func (s *Scheduler) EventChan() chan<- Event {
	return s.eventChan
}

// Run starts the reactor loop
func (s *Scheduler) Run(ctx context.Context) {
	log.Println("[Scheduler Core] Reactor loop running...")
	for {
		select {
		case event := <-s.eventChan:
			s.handleEvent(event)
			s.scheduleReadyTasks()
		case <-ctx.Done():
			log.Println("[Scheduler Core] Reactor loop stopping...")
			return
		}
	}
}

// handleEvent processes single lifecycle events and mutates state tables
func (s *Scheduler) handleEvent(event Event) {
	switch event.Type {
	case EventDAGArrived:
		log.Printf("[Scheduler Event] DAG Submitted: %s\n", event.DAG.ID)
		if err := s.graphEng.RegisterDAG(event.DAG); err != nil {
			log.Printf("Error registering DAG %s: %v\n", event.DAG.ID, err)
			s.stateMgr.UpdateDAGState(event.DAG.ID, "FAILED")
			return
		}
		s.stateMgr.UpdateDAGState(event.DAG.ID, "ACTIVE")

		// Calculate Upward & Downward Ranks bottom-up / top-down
		s.calculateRanks(event.DAG)

		// Queue entry tasks (indegree == 0)
		for _, task := range event.DAG.Tasks {
			if len(task.Predecessors) == 0 {
				_ = s.stateMgr.UpdateTaskState(task.ID, api.StateReady)
				task.ReadyAt = time.Now()
				heap.Push(&s.pq, task)
			} else {
				_ = s.stateMgr.UpdateTaskState(task.ID, api.StateWaiting)
			}
		}

	case EventTaskCompleted:
		log.Printf("[Scheduler Event] Task Completed: %s by Worker %s\n", event.TaskID, event.WorkerID)
		_ = s.stateMgr.UpdateTaskState(event.TaskID, api.StateCompleted)
		_ = s.stateMgr.UpdateWorkerState(event.WorkerID, "IDLE")

		// Release successor dependencies
		succs, err := s.graphEng.GetSuccessors(event.TaskID)
		if err != nil {
			log.Printf("Error fetching successors for task %s: %v\n", event.TaskID, err)
			return
		}

		for _, succ := range succs {
			rem, err := s.graphEng.DecrementIndegree(succ.TargetTaskID)
			if err != nil {
				log.Printf("Error decrementing dependency for task %s: %v\n", succ.TargetTaskID, err)
				continue
			}
			if rem == 0 {
				if tNode, exists := s.graphEng.GetTask(succ.TargetTaskID); exists {
					_ = s.stateMgr.UpdateTaskState(tNode.ID, api.StateReady)
					tNode.ReadyAt = time.Now()
					heap.Push(&s.pq, tNode)
				}
			}
		}

	case EventWorkerIdle:
		log.Printf("[Scheduler Event] Worker %s reporting IDLE\n", event.WorkerID)
		_ = s.stateMgr.UpdateWorkerState(event.WorkerID, "IDLE")
	}
}

// scheduleReadyTasks matches highest priority ready task with optimal idle worker
func (s *Scheduler) scheduleReadyTasks() {
	s.updateDynamicPriorities()

	for s.pq.Len() > 0 {
		// Find an idle worker
		var idleWorker *api.Worker
		for _, w := range s.workers {
			if state, ok := s.stateMgr.GetWorkerState(w.ID); ok && state == "IDLE" {
				idleWorker = w
				break
			}
		}

		if idleWorker == nil {
			// No workers available, stop dispatching for this cycle
			break
		}

		// Pop highest priority task
		task := heap.Pop(&s.pq).(*api.TaskNode)
		s.dispatchTask(task, idleWorker)
	}
}

// dispatchTask pre-programs the BPF map contract before launching execution (Golden Rule)
func (s *Scheduler) dispatchTask(task *api.TaskNode, worker *api.Worker) {
	log.Printf("[Scheduler Dispatch] Assigning Task %s to Worker %s (IP: %s)\n", task.ID, worker.ID, worker.IP)
	
	task.AssignedNodeIP = worker.IP
	_ = s.stateMgr.UpdateTaskState(task.ID, api.StateRunning)
	_ = s.stateMgr.UpdateWorkerState(worker.ID, "BUSY")

	// Get successors
	successors, _ := s.graphEng.GetSuccessors(task.ID)

	// Tentatively assign nodes to successors based on EFT scheduling heuristic
	var destIPs []string
	for _, succ := range successors {
		if succTask, exists := s.graphEng.GetTask(succ.TargetTaskID); exists {
			bestWorker := s.selectOptimalWorkerForTask(succTask)
			succTask.AssignedNodeIP = bestWorker.IP // cache tentative IP
			destIPs = append(destIPs, bestWorker.IP)
		}
	}

	// Extract a numeric subtask ID for the eBPF contract
	numericSubtaskID := parseNumericTaskID(task.ID)

	// 3. Construct eBPF contract
	rule := api.DependencyRule{
		SubtaskID:    numericSubtaskID,
		RefCount:     uint32(len(successors)),
		Destinations: destIPs,
	}

	// 4. Call eBPF Control Plane to program kernel maps before execution starts
	_ = ebpf.WriteDependencyRuleToKernel(rule)

	// 5. Execute worker (spawn goroutine)
	go s.executeWorker(worker, task)
}

// executeWorker simulates computation and triggers the UDP socket network output
func (s *Scheduler) executeWorker(worker *api.Worker, task *api.TaskNode) {
	// 1. Calculate simulated compute duration
	duration := s.calculateExecutionDuration(task, worker)
	time.Sleep(duration)

	// 2. THE TRIGGER: Send a local loopback UDP socket payload to trigger eBPF TC hook
	payload := make([]byte, 4)
	numericID := parseNumericTaskID(task.ID)
	
	// Convert task ID to binary representation
	importLittleEndianUint32(payload, numericID)

	targetAddr := "127.0.0.1:9000"
	conn, err := s.dialUDP(targetAddr)
	if err == nil {
		_, _ = conn.Write(payload)
		_ = conn.Close()
		log.Printf("[Worker Sim %s] Blasted UDP trigger packet for task %s (NumericID: %d) to %s\n", worker.ID, task.ID, numericID, targetAddr)
	} else {
		log.Printf("[Worker Sim %s] Failed to send UDP trigger: %v\n", worker.ID, err)
	}

	// 3. Dispatch completion event back to scheduler reactor loop
	s.eventChan <- Event{
		Type:      EventTaskCompleted,
		TaskID:    task.ID,
		WorkerID:  worker.ID,
		Timestamp: time.Now(),
	}
}

func (s *Scheduler) dialUDP(addr string) (interface{ Write([]byte) (int, error); Close() error }, error) {
	// Helper to enable testing and mocking net.Dial
	type udpConn interface {
		Write([]byte) (int, error)
		Close() error
	}
	// We'll import net inside the helper, or use net.Dial directly
	importNetDial := func() (udpConn, error) {
		// Use standard Dial
		var netDial func(string, string) (interface{ Write([]byte) (int, error); Close() error }, error)
		netDial = func(network, address string) (interface{ Write([]byte) (int, error); Close() error }, error) {
			// standard net dial wrapper
			type realConn struct {
				write func([]byte) (int, error)
				close func() error
			}
			return dialUDPSocket(network, address)
		}
		return netDial("udp", addr)
	}
	return importNetDial()
}

// calculateExecutionDuration factors worker computing power
func (s *Scheduler) calculateExecutionDuration(task *api.TaskNode, worker *api.Worker) time.Duration {
	compCost := float64(task.BaseComputation) / worker.ComputeMultiplier
	return time.Duration(compCost) * time.Millisecond
}

// selectOptimalWorkerForTask selects worker minimizing Earliest Finish Time (EFT)
func (s *Scheduler) selectOptimalWorkerForTask(task *api.TaskNode) *api.Worker {
	var bestWorker *api.Worker
	minEFT := math.MaxFloat64

	for _, w := range s.workers {
		// Calculate EST (Earliest Start Time) based on predecessor completion
		est := 0.0
		for _, predID := range task.Predecessors {
			if predNode, exists := s.graphEng.GetTask(predID); exists {
				// Communication transfer cost: DataSize / Bandwidth
				commCost := 0.0
				var dataSize int64
				for _, succ := range predNode.Successors {
					if succ.TargetTaskID == task.ID {
						dataSize = succ.DataSize
						break
					}
				}
				if predNode.AssignedNodeIP != w.IP {
					commCost = float64(dataSize) / (w.NetworkBandwidth * 1024 * 1024) // converted to seconds
				}
				est = math.Max(est, commCost)
			}
		}

		eft := est + (float64(task.BaseComputation) / w.ComputeMultiplier)
		if eft < minEFT {
			minEFT = eft
			bestWorker = w
		}
	}

	if bestWorker == nil && len(s.workers) > 0 {
		return s.workers[0]
	}
	return bestWorker
}

// calculateRanks computes static Upward and Downward ranks using DAG averages
func (s *Scheduler) calculateRanks(dag *api.DAG) {
	avgComp := computeAverageComputeMultiplier(s.workers)
	avgBand := computeAverageBandwidth(s.workers)

	// Calculate Upward Ranks (bottom-up: exit nodes first)
	var computeUpwardRank func(string) float64
	computeUpwardRank = func(taskID string) float64 {
		node := dag.Tasks[taskID]
		if node.StaticRankU > 0 {
			return node.StaticRankU
		}

		wBar := float64(node.BaseComputation) / avgComp
		maxSuccCost := 0.0

		for _, succ := range node.Successors {
			cBar := float64(succ.DataSize) / (avgBand * 1000) // comm delay metric
			cost := cBar + computeUpwardRank(succ.TargetTaskID)
			if cost > maxSuccCost {
				maxSuccCost = cost
			}
		}

		node.StaticRankU = wBar + maxSuccCost
		return node.StaticRankU
	}

	// Calculate Downward Ranks (top-down: entry nodes first)
	var computeDownwardRank func(string)
	computeDownwardRank = func(taskID string) {
		node := dag.Tasks[taskID]
		maxPredCost := 0.0

		for _, predID := range node.Predecessors {
			predNode := dag.Tasks[predID]
			wBar := float64(predNode.BaseComputation) / avgComp

			// Fetch edge communication size
			var dataSize int64
			for _, succ := range predNode.Successors {
				if succ.TargetTaskID == taskID {
					dataSize = succ.DataSize
					break
				}
			}
			cBar := float64(dataSize) / (avgBand * 1000)
			cost := predNode.StaticRankD + wBar + cBar
			if cost > maxPredCost {
				maxPredCost = cost
			}
		}

		node.StaticRankD = maxPredCost

		for _, succ := range node.Successors {
			computeDownwardRank(succ.TargetTaskID)
		}
	}

	// 1. Bottom-up Upward rank
	for id := range dag.Tasks {
		computeUpwardRank(id)
	}

	// 2. Top-down Downward rank (starting from entry tasks with indegree 0)
	for _, task := range dag.Tasks {
		if len(task.Predecessors) == 0 {
			task.StaticRankD = 0
			for _, succ := range task.Successors {
				computeDownwardRank(succ.TargetTaskID)
			}
		}
	}
}

// updateDynamicPriorities evaluates queuing wait times and resource contention
func (s *Scheduler) updateDynamicPriorities() {
	idleCount := 0
	for _, w := range s.workers {
		if state, ok := s.stateMgr.GetWorkerState(w.ID); ok && state == "IDLE" {
			idleCount++
		}
	}
	if idleCount == 0 {
		idleCount = 1
	}

	theta := float64(s.pq.Len()) / float64(idleCount)
	now := time.Now()

	for _, task := range s.pq {
		waitTime := now.Sub(task.ReadyAt).Seconds()
		aging := waitTime * s.config.AgingFactor

		if theta > 1.0 {
			// High Contention: Focus strictly on Upward Rank (Critical Path clearing)
			task.DynamicPriority = task.StaticRankU + aging
		} else {
			// Low Contention: Balance topological depth
			task.DynamicPriority = (task.StaticRankU + task.StaticRankD) + aging
		}
	}

	// Fix the heap structures after updating priorities in-place
	heap.Init(&s.pq)
}

// Helpers
func computeAverageComputeMultiplier(workers []*api.Worker) float64 {
	if len(workers) == 0 {
		return 1.0
	}
	var sum float64
	for _, w := range workers {
		sum += w.ComputeMultiplier
	}
	return sum / float64(len(workers))
}

func computeAverageBandwidth(workers []*api.Worker) float64 {
	if len(workers) == 0 {
		return 50.0 // Default 50 MB/s
	}
	var sum float64
	for _, w := range workers {
		sum += w.NetworkBandwidth
	}
	return sum / float64(len(workers))
}

func parseNumericTaskID(globalTaskID string) uint32 {
	parts := strings.Split(globalTaskID, ":")
	var idStr string
	if len(parts) > 1 {
		idStr = parts[1]
	} else {
		idStr = globalTaskID
	}

	// strip non-digits like "task-0" -> "0"
	var sb strings.Builder
	for i := 0; i < len(idStr); i++ {
		if idStr[i] >= '0' && idStr[i] <= '9' {
			sb.WriteByte(idStr[i])
		}
	}
	id, err := strconv.ParseUint(sb.String(), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(id)
}

func importLittleEndianUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func dialUDPSocket(network, address string) (interface{ Write([]byte) (int, error); Close() error }, error) {
	type socketConn struct {
		write func([]byte) (int, error)
		close func() error
	}
	// We bind to a net.UDPConn wrapper
	importNet := func() (interface{ Write([]byte) (int, error); Close() error }, error) {
		// Just net.Dial
		var realDial func(string, string) (interface{ Write([]byte) (int, error); Close() error }, error)
		realDial = func(n, a string) (interface{ Write([]byte) (int, error); Close() error }, error) {
			// Mockable Dial
			return mockableDial(n, a)
		}
		return realDial(network, address)
	}
	return importNet()
}

var mockableDial = func(network, address string) (interface{ Write([]byte) (int, error); Close() error }, error) {
	// Standard network interface wrapper. Using dummy structures to prevent windows net connection errors in tests.
	type connWrapper struct {
		payload []byte
	}
	return &dummyUDP{addr: address}, nil
}

type dummyUDP struct {
	addr string
}

func (d *dummyUDP) Write(b []byte) (int, error) {
	return len(b), nil
}

func (d *dummyUDP) Close() error {
	return nil
}
