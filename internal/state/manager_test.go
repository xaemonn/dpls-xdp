package state

import (
	"testing"

	"dpls-xdp/pkg/api"
)

func TestStateTransitions(t *testing.T) {
	manager := NewManager()
	taskID := "dag1:task0"

	// 1. Initial State
	err := manager.UpdateTaskState(taskID, api.StateWaiting)
	if err != nil {
		t.Fatalf("failed initial state: %v", err)
	}

	// 2. Valid transition WAITING -> READY
	err = manager.UpdateTaskState(taskID, api.StateReady)
	if err != nil {
		t.Fatalf("expected valid transition WAITING->READY, got: %v", err)
	}

	// 3. Invalid transition READY -> COMPLETED
	err = manager.UpdateTaskState(taskID, api.StateCompleted)
	if err == nil {
		t.Fatal("expected error on transition READY->COMPLETED, got none")
	}

	// 4. Valid transition READY -> RUNNING
	err = manager.UpdateTaskState(taskID, api.StateRunning)
	if err != nil {
		t.Fatalf("expected valid transition READY->RUNNING, got: %v", err)
	}

	// 5. Valid transition RUNNING -> COMPLETED
	err = manager.UpdateTaskState(taskID, api.StateCompleted)
	if err != nil {
		t.Fatalf("expected valid transition RUNNING->COMPLETED, got: %v", err)
	}
}
