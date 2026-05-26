package graph

import (
	"testing"

	"dpls-xdp/pkg/api"
)

func TestGraphEngineOperations(t *testing.T) {
	engine := NewEngine()

	// Construct mock DAG
	dag := &api.DAG{
		ID: "dag1",
		Tasks: map[string]*api.TaskNode{
			"dag1:task0": {
				ID:              "dag1:task0",
				BaseComputation: 100,
				Successors: []api.Dependency{
					{TargetTaskID: "dag1:task1", DataSize: 100},
				},
				State: api.StateWaiting,
			},
			"dag1:task1": {
				ID:                    "dag1:task1",
				BaseComputation: 100,
				Predecessors:          []string{"dag1:task0"},
				RemainingDependencies: 1,
				State:                 api.StateWaiting,
			},
		},
	}

	err := engine.RegisterDAG(dag)
	if err != nil {
		t.Fatalf("failed to register DAG: %v", err)
	}

	// Double registration should fail
	err = engine.RegisterDAG(dag)
	if err == nil {
		t.Fatal("expected error on duplicate DAG registration, got none")
	}

	// GetTask verification
	task, exists := engine.GetTask("dag1:task0")
	if !exists {
		t.Fatal("expected task0 to exist")
	}
	if task.BaseComputation != 100 {
		t.Errorf("expected computation 100, got %d", task.BaseComputation)
	}

	// Decrement Indegree verification
	rem, err := engine.DecrementIndegree("dag1:task1")
	if err != nil {
		t.Fatalf("failed to decrement indegree: %v", err)
	}
	if rem != 0 {
		t.Errorf("expected 0 remaining dependencies, got %d", rem)
	}

	// Check successors
	succs, err := engine.GetSuccessors("dag1:task0")
	if err != nil {
		t.Fatalf("failed to get successors: %v", err)
	}
	if len(succs) != 1 || succs[0].TargetTaskID != "dag1:task1" {
		t.Errorf("invalid successor details: %v", succs)
	}
}
