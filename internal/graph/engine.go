package graph

import (
	"fmt"
	"sync"

	"dpls-xdp/pkg/api"
)

// Engine manages registered DAGs and task node lookup/mutations.
type Engine struct {
	mu   sync.RWMutex
	dags map[string]*api.DAG
}

// NewEngine creates a new Graph Engine.
func NewEngine() *Engine {
	return &Engine{
		dags: make(map[string]*api.DAG),
	}
}

// RegisterDAG stores a validated DAG topology.
func (e *Engine) RegisterDAG(dag *api.DAG) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.dags[dag.ID]; exists {
		return fmt.Errorf("DAG %s already registered", dag.ID)
	}

	e.dags[dag.ID] = dag
	return nil
}

// GetTask returns a pointer to a TaskNode by its global ID.
func (e *Engine) GetTask(taskID string) (*api.TaskNode, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Parse DAG ID from global task ID (format "dag_id:task_id")
	for _, dag := range e.dags {
		if node, exists := dag.Tasks[taskID]; exists {
			return node, true
		}
	}
	return nil, false
}

// DecrementIndegree decrements the remaining dependencies counter of a task and returns the new value.
func (e *Engine) DecrementIndegree(taskID string) (int32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, dag := range e.dags {
		if node, exists := dag.Tasks[taskID]; exists {
			if node.RemainingDependencies > 0 {
				node.RemainingDependencies--
			}
			return node.RemainingDependencies, nil
		}
	}
	return -1, fmt.Errorf("task %s not found", taskID)
}

// GetSuccessors retrieves the direct successor list for a task.
func (e *Engine) GetSuccessors(taskID string) ([]api.Dependency, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, dag := range e.dags {
		if node, exists := dag.Tasks[taskID]; exists {
			// Return a copy to avoid concurrent slice access issues
			copied := make([]api.Dependency, len(node.Successors))
			copy(copied, node.Successors)
			return copied, nil
		}
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

// GetPredecessors retrieves the direct predecessor list for a task.
func (e *Engine) GetPredecessors(taskID string) ([]string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, dag := range e.dags {
		if node, exists := dag.Tasks[taskID]; exists {
			copied := make([]string, len(node.Predecessors))
			copy(copied, node.Predecessors)
			return copied, nil
		}
	}
	return nil, fmt.Errorf("task %s not found", taskID)
}

// GetDAG returns the registered DAG struct by ID.
func (e *Engine) GetDAG(dagID string) (*api.DAG, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	dag, exists := e.dags[dagID]
	return dag, exists
}
