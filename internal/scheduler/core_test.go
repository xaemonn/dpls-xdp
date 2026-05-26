package scheduler

import (
	"context"
	"testing"
	"time"

	"dpls-xdp/internal/graph"
	"dpls-xdp/internal/state"
	"dpls-xdp/pkg/api"
)

func TestSchedulerEndToEnd(t *testing.T) {
	graphEng := graph.NewEngine()
	stateMgr := state.NewManager()

	workers := []*api.Worker{
		{ID: "worker-1", IP: "127.0.0.1", ComputeMultiplier: 1.5, NetworkBandwidth: 100},
		{ID: "worker-2", IP: "127.0.0.2", ComputeMultiplier: 1.0, NetworkBandwidth: 50},
	}

	for _, w := range workers {
		_ = stateMgr.UpdateWorkerState(w.ID, "IDLE")
	}

	sched := NewScheduler(graphEng, stateMgr, workers, 0.5)

	// Mock DAG: task0 -> task1
	dag := &api.DAG{
		ID: "testdag",
		Tasks: map[string]*api.TaskNode{
			"testdag:task0": {
				ID:              "testdag:task0",
				BaseComputation: 1500, // 1000ms on 1.5x worker
				Successors: []api.Dependency{
					{TargetTaskID: "testdag:task1", DataSize: 500},
				},
				Predecessors:          []string{},
				RemainingDependencies: 0,
				State:                 api.StateWaiting,
			},
			"testdag:task1": {
				ID:                    "testdag:task1",
				BaseComputation: 1000, // 666ms on 1.5x worker
				Successors:            []api.Dependency{},
				Predecessors:          []string{"testdag:task0"},
				RemainingDependencies: 1,
				State:                 api.StateWaiting,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Launch Scheduler in background
	go sched.Run(ctx)

	// Ingest DAG
	sched.SubmitDAG(dag)

	// Wait briefly for event loops to process task0
	time.Sleep(100 * time.Millisecond)

	// task0 should have been popped from PQ and is now RUNNING on one of the workers
	task0State, _ := stateMgr.GetTaskState("testdag:task0")
	if task0State != api.StateRunning {
		t.Errorf("expected task0 to be RUNNING, got %s", task0State)
	}

	// Verify scheduler calculated ranks
	task0, _ := graphEng.GetTask("testdag:task0")
	if task0.StaticRankU <= 0 {
		t.Errorf("expected task0 Upward Rank > 0, got %f", task0.StaticRankU)
	}

	// Wait for task0 completion (takes 1000ms, we wait 1100ms more)
	time.Sleep(1100 * time.Millisecond)

	// task0 should be completed, task1 should be running
	task0State, _ = stateMgr.GetTaskState("testdag:task0")
	if task0State != api.StateCompleted {
		t.Errorf("expected task0 to be COMPLETED, got %s", task0State)
	}

	task1State, _ := stateMgr.GetTaskState("testdag:task1")
	if task1State != api.StateRunning {
		t.Errorf("expected task1 to be RUNNING, got %s", task1State)
	}

	// Wait for task1 completion (takes 666ms, we wait 800ms)
	time.Sleep(800 * time.Millisecond)

	task1State, _ = stateMgr.GetTaskState("testdag:task1")
	if task1State != api.StateCompleted {
		t.Errorf("expected task1 to be COMPLETED, got %s", task1State)
	}
}

func TestParseNumericTaskID(t *testing.T) {
	tests := []struct {
		id       string
		expected uint32
	}{
		{"dag1:task-0", 0},
		{"dag1:task-99", 99},
		{"12", 12},
		{"task-1234", 1234},
	}

	for _, tc := range tests {
		res := parseNumericTaskID(tc.id)
		if res != tc.expected {
			t.Errorf("for id %s expected %d, got %d", tc.id, tc.expected, res)
		}
	}
}
