package main

import (
	"context"
	"flag"
	"log"
	"time"

	"dpls-xdp/internal/ebpf"
	"dpls-xdp/internal/graph"
	"dpls-xdp/internal/scheduler"
	"dpls-xdp/internal/state"
	"dpls-xdp/pkg/api"
)

func main() {
	mode := flag.String("mode", "mock", "Execution mode: mock or ebpf")
	iface := flag.String("interface", "lo", "Network interface for eBPF attachment")
	elfPath := flag.String("ebpf-elf", "internal/ebpf/c/tc_bridge.o", "Path to compiled eBPF ELF file")
	flag.Parse()

	log.Printf("[DPLS Main] Starting in %s mode", *mode)

	if *mode == "ebpf" {
		// eBPF Setup
		log.Printf("[DPLS Main] Loading eBPF objects from %s", *elfPath)
		if err := ebpf.LoadBPFObjects(*elfPath); err != nil {
			log.Fatalf("Failed to load eBPF objects: %v", err)
		}

		log.Printf("[DPLS Main] Attaching TC to interface %s", *iface)
		if err := ebpf.AttachTC(*iface); err != nil {
			log.Fatalf("Failed to attach TC: %v", err)
		}
		defer func() {
			log.Printf("[DPLS Main] Detaching TC from %s", *iface)
			ebpf.DetachTC()
		}()
	}

	// Initialize Scheduler components
	graphEng := graph.NewEngine()
	stateMgr := state.NewManager()

	workers := []*api.Worker{
		{ID: "worker-1", IP: "127.0.0.1", ComputeMultiplier: 1.5, NetworkBandwidth: 100},
		{ID: "worker-2", IP: "127.0.0.2", ComputeMultiplier: 1.0, NetworkBandwidth: 50},
	}

	for _, w := range workers {
		_ = stateMgr.UpdateWorkerState(w.ID, "IDLE")
	}

	sched := scheduler.NewScheduler(graphEng, stateMgr, workers, 0.5)

	// Create a 2-node linear DAG: task0 -> task1
	dag := &api.DAG{
		ID: "testdag",
		Tasks: map[string]*api.TaskNode{
			"task0": {
				ID:              "task0",
				BaseComputation: 100,
				Predecessors:    []string{},
				Successors: []api.Dependency{
					{TargetTaskID: "task1", DataSize: 1024},
				},
			},
			"task1": {
				ID:              "task1",
				BaseComputation: 50,
				Predecessors:    []string{"task0"},
				Successors:      []api.Dependency{},
			},
		},
	}

	_ = graphEng.RegisterDAG(dag)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("[DPLS Main] Starting Scheduler...")
	go sched.Run(ctx)

	start := time.Now()
	sched.SubmitDAG(dag)

	// Wait for DAG completion
	for {
		state, exists := stateMgr.GetDAGState(dag.ID)
		if exists && state == "COMPLETED" {
			log.Printf("Task Completed. Duration: %v", time.Since(start))
			break
		}
		if ctx.Err() != nil {
			log.Printf("Timeout waiting for DAG to complete")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}
