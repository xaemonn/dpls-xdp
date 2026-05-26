package state

import (
	"fmt"
	"sync"

	"dpls-xdp/pkg/api"
)

// Manager coordinates state transitions and tracks task, DAG, and worker states.
type Manager struct {
	mu           sync.RWMutex
	taskStates   map[string]api.TaskState
	dagStates    map[string]string
	workerStates map[string]string // "IDLE" or "BUSY"
}

// NewManager creates a new State Manager.
func NewManager() *Manager {
	return &Manager{
		taskStates:   make(map[string]api.TaskState),
		dagStates:    make(map[string]string),
		workerStates: make(map[string]string),
	}
}

// UpdateTaskState transition check and update.
func (m *Manager) UpdateTaskState(taskID string, newState api.TaskState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	currentState, exists := m.taskStates[taskID]
	if !exists {
		// Initialize task state if first time
		m.taskStates[taskID] = newState
		return nil
	}

	// Validate valid state transitions
	switch currentState {
	case api.StateWaiting:
		if newState != api.StateReady && newState != api.StateFailed {
			return fmt.Errorf("invalid transition: WAITING -> %s", newState)
		}
	case api.StateReady:
		if newState != api.StateRunning && newState != api.StateFailed {
			return fmt.Errorf("invalid transition: READY -> %s", newState)
		}
	case api.StateRunning:
		if newState != api.StateCompleted && newState != api.StateFailed {
			return fmt.Errorf("invalid transition: RUNNING -> %s", newState)
		}
	case api.StateFailed:
		if newState != api.StateReady {
			return fmt.Errorf("invalid transition: FAILED -> %s", newState)
		}
	case api.StateCompleted:
		return fmt.Errorf("terminal state: COMPLETED cannot transition to %s", newState)
	}

	m.taskStates[taskID] = newState
	return nil
}

// GetTaskState retrieves current state of a task.
func (m *Manager) GetTaskState(taskID string) (api.TaskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.taskStates[taskID]
	return state, exists
}

// UpdateDAGState updates a parent DAG lifecycle state.
func (m *Manager) UpdateDAGState(dagID string, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dagStates[dagID] = state
}

// GetDAGState returns DAG lifecycle status.
func (m *Manager) GetDAGState(dagID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.dagStates[dagID]
	return state, exists
}

// UpdateWorkerState transitions worker between IDLE and BUSY.
func (m *Manager) UpdateWorkerState(workerID string, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if state != "IDLE" && state != "BUSY" {
		return fmt.Errorf("invalid worker state: %s", state)
	}

	m.workerStates[workerID] = state
	return nil
}

// GetWorkerState returns current state of worker.
func (m *Manager) GetWorkerState(workerID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.workerStates[workerID]
	return state, exists
}
