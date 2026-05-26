package worker

import (
	"fmt"
	"sync"

	"dpls-xdp/pkg/api"
)

// Manager coordinates worker registrations and capacity attributes
type Manager struct {
	mu      sync.RWMutex
	workers map[string]*api.Worker
}

// NewManager creates a new worker manager registry
func NewManager() *Manager {
	return &Manager{
		workers: make(map[string]*api.Worker),
	}
}

// RegisterWorker registers a new heterogeneous worker profile
func (m *Manager) RegisterWorker(w *api.Worker) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.workers[w.ID]; exists {
		return fmt.Errorf("worker %s already registered", w.ID)
	}

	m.workers[w.ID] = w
	return nil
}

// GetWorker retrieves worker by ID
func (m *Manager) GetWorker(workerID string) (*api.Worker, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w, exists := m.workers[workerID]
	return w, exists
}

// ListWorkers returns a slice of all registered workers
func (m *Manager) ListWorkers() []*api.Worker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*api.Worker, 0, len(m.workers))
	for _, w := range m.workers {
		list = append(list, w)
	}
	return list
}
