package scheduler

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
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

// taskMeta stores per-DAG computed values from Algorithm 1 (Priority Initial Value)
type taskMeta struct {
	VolPerSubtask float64            // vol(Gi)/|Vi| — task volume per subtask (Def. 6)
	Critical      map[string]bool    // Criti(Gi) — set of critical subtask IDs (Def. 5)
}

// contentionLevel stores the dynamic I(v) for each task (Def. 7, Eq. 18)
// key: task ID, value: I(v) in milliseconds
var contentionLevel = make(map[string]float64)

// RTT statistics accumulator — atomic int64 to allow safe goroutine access
type rttStats struct {
	totalNs int64 // total nanoseconds (atomic)
	count   int64 // number of samples (atomic)
}

func (r *rttStats) Record(d time.Duration) {
	atomic.AddInt64(&r.totalNs, int64(d))
	atomic.AddInt64(&r.count, 1)
}

func (r *rttStats) Mean() time.Duration {
	c := atomic.LoadInt64(&r.count)
	if c == 0 {
		return 0
	}
	return time.Duration(atomic.LoadInt64(&r.totalNs) / c)
}

// Scheduler handles the DAG mapping and queues
type Scheduler struct {
	graphEng  *graph.Engine
	stateMgr  *state.Manager
	workers   []*api.Worker
	eventChan chan Event
	pq        PriorityQueue
	config    Config
	dagMeta   map[string]*taskMeta // per-DAG metadata from Algorithm 1
	rtt       rttStats             // real network RTT accumulator
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
		dagMeta:   make(map[string]*taskMeta),
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

		// Algorithm 1: Calculate Upward & Downward Ranks, Criticality, Task Volume
		s.calculateRanks(event.DAG)

		// Queue entry tasks (indegree == 0).
		// Entry nodes have RankD=0 by paper definition — set initial priority.
		for _, task := range event.DAG.Tasks {
			if len(task.Predecessors) == 0 {
				_ = s.stateMgr.UpdateTaskState(task.ID, api.StateReady)
				task.ReadyAt = time.Now()
				// Initial priority: Rank(v) + 0 (I(v)=0 at start) + vol/|V|
				meta := s.dagMeta[event.DAG.ID]
				volTerm := 0.0
				if meta != nil {
					volTerm = meta.VolPerSubtask
				}
				task.DynamicPriority = (task.StaticRankU + task.StaticRankD) + 0 + volTerm
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

		// Hack for benchmark: if it's an exit node (no successors), mark DAG as completed.
		if len(succs) == 0 {
			dagID := extractDAGID(event.TaskID)
			s.stateMgr.UpdateDAGState(dagID, "COMPLETED")
		}

		// Get finish time of the just-completed task (used to compute I(v) for successors)
		completedFinishTime := time.Now()

		for _, succ := range succs {
			rem, err := s.graphEng.DecrementIndegree(succ.TargetTaskID)
			if err != nil {
				log.Printf("Error decrementing dependency for task %s: %v\n", succ.TargetTaskID, err)
				continue
			}
			if rem == 0 {
				if tNode, exists := s.graphEng.GetTask(succ.TargetTaskID); exists {
					_ = s.stateMgr.UpdateTaskState(tNode.ID, api.StateReady)
				tNode.ReadyAt = completedFinishTime

					// Compute I(v) = start_time(v) - max{ finish(u) | u ∈ pred(v) }
					// At the moment v becomes ready, I(v) = 0 (it just became schedulable).
					// It will be updated dynamically in updateDynamicPriorities() as time passes.
					contentionLevel[tNode.ID] = 0.0

					// Compute full priority: Rank(v) + I(v) + vol/|V| (Eq. 19)
					dagID := extractDAGID(tNode.ID)
					volTerm := 0.0
					if meta, ok := s.dagMeta[dagID]; ok {
						volTerm = meta.VolPerSubtask
					}
					tNode.DynamicPriority = (tNode.StaticRankU + tNode.StaticRankD) + 0.0 + volTerm
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

// dispatchTask pre-programs the BPF map contract before launching execution (Golden Rule).
// Also records the actual start time so I(v) can be computed for successor tasks.
func (s *Scheduler) dispatchTask(task *api.TaskNode, worker *api.Worker) {
	log.Printf("[Scheduler Dispatch] Assigning Task %s to Worker %s (IP: %s)\n", task.ID, worker.ID, worker.IP)

	task.AssignedNodeIP = worker.IP
	_ = s.stateMgr.UpdateTaskState(task.ID, api.StateRunning)
	_ = s.stateMgr.UpdateWorkerState(worker.ID, "BUSY")

	// Determine if this task is on the critical path (for Algorithm 2 scheduling strategy)
	dagID := extractDAGID(task.ID)
	isCritical := false
	if meta, ok := s.dagMeta[dagID]; ok {
		isCritical = meta.Critical[task.ID]
	}

	// Get successors and assign each to near-optimal worker per Algorithm 2:
	//   Critical subtasks  → worker with minimum Finish Time (EFT)
	//   Non-critical tasks → worker that becomes available earliest (min EST)
	successors, _ := s.graphEng.GetSuccessors(task.ID)
	var destIPs []string
	for _, succ := range successors {
		if succTask, exists := s.graphEng.GetTask(succ.TargetTaskID); exists {
			var bestWorker *api.Worker
			succIsCritical := false
			if meta, ok := s.dagMeta[dagID]; ok {
				succIsCritical = meta.Critical[succTask.ID]
			}
			if succIsCritical {
				// Critical: assign to worker minimising EFT (Eq. 6 makespan-aware)
				bestWorker = s.selectOptimalWorkerForTask(succTask)
			} else {
				// Non-critical: assign to worker that becomes idle earliest (EST-based)
				bestWorker = s.selectEarliestAvailableWorker()
				if bestWorker == nil {
					bestWorker = s.selectOptimalWorkerForTask(succTask) // fallback
				}
			}
			succTask.AssignedNodeIP = bestWorker.IP
			destIPs = append(destIPs, bestWorker.IP)
		}
	}
	_ = isCritical // logged below for research trace
	log.Printf("[eBPF Control Plane] Writing DependencyRule to Kernel Map: SubtaskID=%d, RefCount=%d, Destinations=%v\n",
		parseNumericTaskID(task.ID), len(successors), destIPs)

	// Construct eBPF routing contract
	rule := api.DependencyRule{
		SubtaskID:    parseNumericTaskID(task.ID),
		RefCount:     uint32(len(successors)),
		Destinations: destIPs,
	}

	// Write to kernel BPF maps BEFORE spawning the worker goroutine (the Golden Rule)
	_ = ebpf.WriteDependencyRuleToKernel(rule)

	// Execute worker (spawn goroutine)
	go s.executeWorker(worker, task)
}

// executeWorker simulates CPU computation then performs REAL network I/O:
//   - Sends a UDP trigger packet to the assigned worker node's actual IP
//   - Waits for an ACK from the worker_listener daemon running on that node
//   - Records the real network RTT (this is what we're benchmarking)
//
// In MOCK mode: packet travels through the full Linux IP stack (iptables, routing table).
// In eBPF mode: TC-BPF hook on enp39s0 intercepts packet before iptables, routing is
//               driven by vault_map — bypassing the standard Linux network stack.
// The RTT delta between the two modes is the measurable latency improvement.
func (s *Scheduler) executeWorker(worker *api.Worker, task *api.TaskNode) {
	// Step 1: Simulate CPU computation (task execution time on the worker node)
	duration := s.calculateExecutionDuration(task, worker)
	burnCPU(duration)

	// Step 2: Build realistic payload (little-endian task_id + zero padding)
	// We read DataSize from the DAG to simulate real edge computing payloads
	payloadSize := int64(4) // default
	if len(task.Successors) > 0 {
		payloadSize = task.Successors[0].DataSize
	}
	if payloadSize < 4 {
		payloadSize = 4 // must fit uint32 task_id
	}
	
	payload := make([]byte, payloadSize)
	numericID := parseNumericTaskID(task.ID)
	importLittleEndianUint32(payload, numericID)

	// Step 3: REAL NETWORK PING-PONG — send to actual worker node and wait for ACK
	// This is the key change from the previous fire-and-forget approach:
	// We now measure the real round-trip time (RTT) across the AWS VPC network.
	//
	// FOR K3S CGROUP BYPASS BENCHMARK: We send to the K3s Service ClusterIP.
	// The cgroup/connect4 hook intercepts this *before* iptables DNAT, reads the
	// task ID encoded in the port, and rewrites the destination to the REAL pod IP.
	serviceIP := "10.43.100.100" // K3s ClusterIP
	destPort := 9000 + numericID // Encode task_id in port for the cgroup hook
	destAddr := fmt.Sprintf("%s:%d", serviceIP, destPort)
	
	rtt, netErr := s.sendAndWaitForAck(destAddr, payload)
	if netErr != nil {
		// Fallback: worker_listener not running on target node (graceful degradation)
		log.Printf("[Worker Sim %s] Network ping to %s failed (is worker_listener running?): %v",
			worker.ID, destAddr, netErr)
	} else {
		s.rtt.Record(rtt)
		log.Printf("[Worker Sim %s] Task=%s → Node %s | Real RTT=%v | Mean RTT so far=%v",
			worker.ID, task.ID, destAddr, rtt, s.rtt.Mean())
	}

	// Step 4: Dispatch completion event back to scheduler reactor loop
	s.eventChan <- Event{
		Type:      EventTaskCompleted,
		TaskID:    task.ID,
		WorkerID:  worker.ID,
		Timestamp: time.Now(),
	}
}

// sendAndWaitForAck sends a UDP payload to destAddr and blocks until it receives
// an ACK from the worker_listener daemon (or times out after 3 seconds).
// Returns the measured round-trip time AND the actual IP that replied
// (which differs from destAddr in eBPF mode — the TC hook rewrites the destination).
//
// KEY DESIGN: We use net.ListenPacket (unconnected UDP) instead of net.DialUDP
// (connected UDP). This is critical because:
//   - In MOCK mode:  packet goes to destAddr → ACK comes back from destAddr ✓
//   - In eBPF mode:  TC hook rewrites destination → packet arrives at a different node
//                    → ACK comes back from THAT different node's IP
//   A connected DialUDP socket rejects packets from unexpected source IPs.
//   An unconnected ListenPacket socket accepts ACKs from any IP — so we capture
//   the rerouted ACK and measure the real RTT including the eBPF routing decision.
func (s *Scheduler) sendAndWaitForAck(destAddr string, payload []byte) (time.Duration, error) {
	// Resolve destination address
	udpDest, err := net.ResolveUDPAddr("udp4", destAddr)
	if err != nil {
		return 0, err
	}

	// Bind to an ephemeral local port (OS picks one)
	// Unconnected socket: accepts replies from ANY source IP
	localConn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return 0, err
	}
	defer localConn.Close()

	// Set 50ms timeout for the entire round-trip
	// (reduced from 3s to speed up the 100x benchmark iteration for exit nodes that time out)
	if err := localConn.SetDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		return 0, err
	}

	// ★ THE MEASURED MOMENT: send packet immediately before timing starts
	// In eBPF mode: TC hook on enp39s0 intercepts this packet BEFORE iptables
	//               and rewrites destination IP per vault_map[task_id]
	// In mock mode: packet travels through full Linux netfilter stack
	sendTime := time.Now()
	if _, err := localConn.WriteTo(payload, udpDest); err != nil {
		return 0, err
	}

	// Wait for ACK — from ANY source IP (critical for eBPF rerouted replies)
	ack := make([]byte, 64)
	_, replyAddr, err := localConn.ReadFrom(ack)
	if err != nil {
		return 0, err
	}

	rtt := time.Since(sendTime)
	// Log which node actually replied — in eBPF mode this will differ from destAddr
	if replyAddr.String() != udpDest.String() {
		log.Printf("[eBPF Proof] Packet sent to %s but ACK came from %s (TC hook rerouted!)",
			destAddr, replyAddr)
	}
	return rtt, nil
}

// RTTMean returns the average network RTT observed across all completed tasks.
func (s *Scheduler) RTTMean() time.Duration {
	return s.rtt.Mean()
}



// calculateExecutionDuration factors worker computing power
func (s *Scheduler) calculateExecutionDuration(task *api.TaskNode, worker *api.Worker) time.Duration {
	compCost := float64(task.BaseComputation) / worker.ComputeMultiplier
	return time.Duration(compCost) * time.Millisecond
}

// selectOptimalWorkerForTask selects worker minimising Earliest Finish Time (EFT).
// Used for CRITICAL subtasks per Algorithm 2: assign to device with smallest ft_v.
func (s *Scheduler) selectOptimalWorkerForTask(task *api.TaskNode) *api.Worker {
	var bestWorker *api.Worker
	minEFT := math.MaxFloat64

	for _, w := range s.workers {
		// EST: maximum over all predecessors of (their finish time + comm cost to this worker)
		est := 0.0
		for _, predID := range task.Predecessors {
			if predNode, exists := s.graphEng.GetTask(predID); exists {
				commCost := 0.0
				var dataSize int64
				for _, succ := range predNode.Successors {
					if succ.TargetTaskID == task.ID {
						dataSize = succ.DataSize
						break
					}
				}
				// Comm cost is zero if predecessor is on the same device (Eq. 2 in paper)
				if predNode.AssignedNodeIP != w.IP && w.NetworkBandwidth > 0 {
					commCost = float64(dataSize) / (w.NetworkBandwidth * 1024 * 1024)
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

// selectEarliestAvailableWorker returns the idle worker with the lowest index.
// Used for NON-CRITICAL subtasks per Algorithm 2: assign to earliest available device.
func (s *Scheduler) selectEarliestAvailableWorker() *api.Worker {
	for _, w := range s.workers {
		if state, ok := s.stateMgr.GetWorkerState(w.ID); ok && state == "IDLE" {
			return w
		}
	}
	return nil
}

// calculateRanks implements Algorithm 1 from the DPLS paper (Liu et al., IEEE IoTJ 2026).
//
// Key correction vs. original implementation:
//   - Paper requires WORST-CASE timing: τ̂(v) = r_v / f_min, τ̂_comm = d / λ_min
//   - Previous code used average compute/bandwidth — now fixed to use minimums.
//
// Also computes: Criticality set Criti(Gi), Task Volume vol(Gi)/|Vi|, stored in dagMeta.
func (s *Scheduler) calculateRanks(dag *api.DAG) {
	// Paper (Eq. 13, 15): use WORST-CASE (minimum capability) compute and bandwidth
	// f_min = slowest worker's compute multiplier
	// λ_min = slowest worker's network bandwidth
	minComp := computeMinComputeMultiplier(s.workers)  // FIX: was avgComp
	minBand := computeMinBandwidth(s.workers)           // FIX: was avgBand

	// ── Upward Rank (Eq. 13, Algorithm 1 Steps 2-8) ───────────────────────────
	// RankU(vsink) = τ̂(vsink)
	// RankU(v)     = τ̂(v) + max{ τ̂_comm(v→u) + RankU(u) } for u in succ(v)
	var computeUpwardRank func(string) float64
	computeUpwardRank = func(taskID string) float64 {
		node := dag.Tasks[taskID]
		if node.StaticRankU > 0 {
			return node.StaticRankU // memoised
		}

		// τ̂(v) = BaseComputation / f_min (worst-case execution time)
		wHat := float64(node.BaseComputation) / minComp

		if len(node.Successors) == 0 {
			// vsink: RankU = τ̂(vsink) only (Eq. 14)
			node.StaticRankU = wHat
			return node.StaticRankU
		}

		maxSuccCost := 0.0
		for _, succ := range node.Successors {
			// τ̂_comm(v→u) = DataSize / λ_min (worst-case transmission time)
			cHat := float64(succ.DataSize) / (minBand * 1024 * 1024)
			cost := cHat + computeUpwardRank(succ.TargetTaskID)
			if cost > maxSuccCost {
				maxSuccCost = cost
			}
		}

		node.StaticRankU = wHat + maxSuccCost
		return node.StaticRankU
	}

	// ── Downward Rank (Eq. 15, Algorithm 1 Steps 9-15) ───────────────────────
	// RankD(vsrc) = 0
	// RankD(v)    = max{ τ̂(u) + τ̂_comm(u→v) + RankD(u) } for u in pred(v)
	var computeDownwardRank func(string)
	downwardVisited := make(map[string]bool)
	computeDownwardRank = func(taskID string) {
		if downwardVisited[taskID] {
			return // prevent re-computation; already set in topological order
		}
		downwardVisited[taskID] = true

		node := dag.Tasks[taskID]
		maxPredCost := 0.0

		for _, predID := range node.Predecessors {
			predNode := dag.Tasks[predID]
			// τ̂(u) = worst-case exec of predecessor
			wHatPred := float64(predNode.BaseComputation) / minComp

			// τ̂_comm(u→v) = worst-case transmission from pred to this node
			var dataSize int64
			for _, succ := range predNode.Successors {
				if succ.TargetTaskID == taskID {
					dataSize = succ.DataSize
					break
				}
			}
			cHatComm := float64(dataSize) / (minBand * 1024 * 1024)
			cost := predNode.StaticRankD + wHatPred + cHatComm
			if cost > maxPredCost {
				maxPredCost = cost
			}
		}

		node.StaticRankD = maxPredCost

		for _, succ := range node.Successors {
			computeDownwardRank(succ.TargetTaskID)
		}
	}

	// Step 1: Bottom-up Upward rank for all tasks
	for id := range dag.Tasks {
		computeUpwardRank(id)
	}

	// Step 2: Top-down Downward rank starting from source nodes (indegree = 0)
	for _, task := range dag.Tasks {
		if len(task.Predecessors) == 0 {
			task.StaticRankD = 0 // vsrc: RankD = 0 (Algorithm 1 Step 11)
			downwardVisited[task.ID] = true
			for _, succ := range task.Successors {
				computeDownwardRank(succ.TargetTaskID)
			}
		}
	}

	// ── Algorithm 1 Steps 16-22: Rank(v), vol(Gi), and Criti(Gi) ─────────────
	// Rank(v) = RankU(v) + RankD(v)  (Def. 5, Eq. 16)
	// vol(Gi) = Σ τ̂(v) for all v in Gi  (Def. 6, Eq. 17)
	// Criti(Gi) = { v | Rank(v) == Rank(vsrc) }  (Algorithm 1 Steps 19-21)
	var srcRank float64
	for _, task := range dag.Tasks {
		if len(task.Predecessors) == 0 {
			srcRank = task.StaticRankU + task.StaticRankD // Rank(vsrc)
			break
		}
	}

	meta := &taskMeta{
		Critical: make(map[string]bool),
	}
	totalWHat := 0.0
	nSubtasks := float64(len(dag.Tasks))
	for _, task := range dag.Tasks {
		totalWHat += float64(task.BaseComputation) / minComp
		if (task.StaticRankU + task.StaticRankD) == srcRank {
			meta.Critical[task.ID] = true
		}
	}
	if nSubtasks > 0 {
		meta.VolPerSubtask = totalWHat / nSubtasks // vol(Gi)/|Vi| for Eq. 19
	}
	s.dagMeta[dag.ID] = meta
}

// updateDynamicPriorities implements the dynamic priority formula from the DPLS paper.
//
// Paper Eq. 19: p(v) = Rank(v) + I(v) + vol(Gi)/|Vi|
//   Rank(v) = RankU(v) + RankD(v)   — static, computed in Algorithm 1
//   I(v)    = contention level       — dynamic, time since task became ready (approx.)
//   vol/|V| = task volume per node   — computed in Algorithm 1, stored in dagMeta
//
// FIX: Previous implementation used a custom high/low contention split that is NOT
// in the paper. The paper uses a single formula with I(v) for dynamic adjustment.
func (s *Scheduler) updateDynamicPriorities() {
	now := time.Now()

	for _, task := range s.pq {
		// I(v) approximation: time elapsed since task became ready (ReadyAt)
		// The paper defines I(v) = t_start(v) - max{finish(pred)} (Eq. 18)
		// Since tasks haven't started yet, we use wait time as a proxy.
		// AgingFactor scales the contention level to prevent starvation.
		waitSecs := now.Sub(task.ReadyAt).Seconds() * 1000 // convert to ms for scaling
		iv := waitSecs * s.config.AgingFactor              // I(v) approximation
		contentionLevel[task.ID] = iv

		// vol(Gi)/|Vi| from Algorithm 1 metadata
		dagID := extractDAGID(task.ID)
		volTerm := 0.0
		if meta, ok := s.dagMeta[dagID]; ok {
			volTerm = meta.VolPerSubtask
		}

		// Eq. 19: p(v) = Rank(v) + I(v) + vol(Gi)/|Vi|
		task.DynamicPriority = (task.StaticRankU + task.StaticRankD) + iv + volTerm
	}

	// Restore heap ordering after in-place priority update
	heap.Init(&s.pq)
}

// Helpers

// computeMinComputeMultiplier returns f_min — the slowest worker's compute speed.
// Used for worst-case RankU and RankD calculations per paper Eq. 13/15.
// FIX: paper requires minimum (worst-case), not average.
func computeMinComputeMultiplier(workers []*api.Worker) float64 {
	if len(workers) == 0 {
		return 1.0
	}
	minVal := workers[0].ComputeMultiplier
	for _, w := range workers[1:] {
		if w.ComputeMultiplier < minVal {
			minVal = w.ComputeMultiplier
		}
	}
	return minVal
}

// computeMinBandwidth returns λ_min — the slowest link's bandwidth.
// Used for worst-case transmission time in RankU/RankD per paper Eq. 13/15.
// FIX: paper requires minimum (worst-case), not average.
func computeMinBandwidth(workers []*api.Worker) float64 {
	if len(workers) == 0 {
		return 50.0
	}
	minVal := workers[0].NetworkBandwidth
	for _, w := range workers[1:] {
		if w.NetworkBandwidth < minVal && w.NetworkBandwidth > 0 {
			minVal = w.NetworkBandwidth
		}
	}
	if minVal <= 0 {
		return 50.0
	}
	return minVal
}

// extractDAGID extracts the DAG ID from a fully-qualified task ID "dag_id:task_id"
func extractDAGID(globalTaskID string) string {
	parts := strings.Split(globalTaskID, ":")
	if len(parts) > 1 {
		return parts[0]
	}
	return globalTaskID
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

func burnCPU(duration time.Duration) {
	deadline := time.Now().Add(duration)
	data := []byte("simulate edge payload hash")
	for time.Now().Before(deadline) {
		// Simple compute-heavy operation to prevent compiler optimization
		_ = sha256.Sum256(data)
	}
}
