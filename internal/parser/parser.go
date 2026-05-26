package parser

import (
	"encoding/json"
	"fmt"
	"time"

	"dpls-xdp/pkg/api"
)

// IngestedTask represents the task structure in the raw JSON payload
type IngestedTask struct {
	TaskID          string           `json:"task_id"`
	BaseComputation int64            `json:"base_computation"`
	Successors      []api.Dependency `json:"successors"`
}

// IngestedDAG represents the raw DAG structure in JSON
type IngestedDAG struct {
	DAGID       string         `json:"dag_id"`
	ArrivalTime int64          `json:"arrival_time"`
	Tasks       []IngestedTask `json:"tasks"`
}

// Parse parses a raw JSON DAG representation, normalizes IDs, and validates topology
func Parse(jsonData []byte) (*api.DAG, error) {
	var raw IngestedDAG
	if err := json.Unmarshal(jsonData, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	if raw.DAGID == "" {
		return nil, fmt.Errorf("dag_id is required")
	}

	if len(raw.Tasks) == 0 {
		return nil, fmt.Errorf("DAG contains no tasks")
	}

	dag := &api.DAG{
		ID:          raw.DAGID,
		ArrivalTime: time.Unix(raw.ArrivalTime, 0),
		Tasks:       make(map[string]*api.TaskNode),
		State:       "SUBMITTED",
	}

	// 1. First pass: construct TaskNodes and normalize IDs, check local uniqueness
	for _, rawTask := range raw.Tasks {
		if rawTask.TaskID == "" {
			return nil, fmt.Errorf("task_id is required for all tasks")
		}

		globalID := fmt.Sprintf("%s:%s", raw.DAGID, rawTask.TaskID)
		if _, exists := dag.Tasks[globalID]; exists {
			return nil, fmt.Errorf("duplicate task ID detected: %s", rawTask.TaskID)
		}

		if rawTask.BaseComputation <= 0 {
			return nil, fmt.Errorf("base_computation must be > 0 for task: %s", rawTask.TaskID)
		}

		node := &api.TaskNode{
			ID:              globalID,
			BaseComputation: rawTask.BaseComputation,
			Successors:      make([]api.Dependency, len(rawTask.Successors)),
			State:           api.StateWaiting,
		}

		// Normalize successor IDs
		for i, succ := range rawTask.Successors {
			if succ.TargetTaskID == "" {
				return nil, fmt.Errorf("successor target task_id is required")
			}
			if succ.DataSize < 0 {
				return nil, fmt.Errorf("data_size must be >= 0 for successor %s of task %s", succ.TargetTaskID, rawTask.TaskID)
			}
			node.Successors[i] = api.Dependency{
				TargetTaskID: fmt.Sprintf("%s:%s", raw.DAGID, succ.TargetTaskID),
				DataSize:     succ.DataSize,
			}
		}

		dag.Tasks[globalID] = node
	}

	// 2. Validate successors actually exist in the DAG
	for _, node := range dag.Tasks {
		for _, succ := range node.Successors {
			if _, exists := dag.Tasks[succ.TargetTaskID]; !exists {
				return nil, fmt.Errorf("successor task %s of task %s does not exist", succ.TargetTaskID, node.ID)
			}
		}
	}

	// 3. Cycle Detection using DFS color tracing (0 = white, 1 = gray, 2 = black)
	colors := make(map[string]int)
	for id := range dag.Tasks {
		colors[id] = 0 // White (unvisited)
	}

	var hasCycle func(string) bool
	hasCycle = func(id string) bool {
		colors[id] = 1 // Gray (visiting)
		node := dag.Tasks[id]

		for _, succ := range node.Successors {
			if colors[succ.TargetTaskID] == 1 {
				return true // Found gray node -> cycle detected
			}
			if colors[succ.TargetTaskID] == 0 {
				if hasCycle(succ.TargetTaskID) {
					return true
				}
			}
		}

		colors[id] = 2 // Black (fully visited)
		return false
	}

	for id := range dag.Tasks {
		if colors[id] == 0 {
			if hasCycle(id) {
				return nil, fmt.Errorf("cycle detected in DAG")
			}
		}
	}

	// 4. Populate Predecessor links for convenience in graph engine
	for id, node := range dag.Tasks {
		for _, succ := range node.Successors {
			succNode := dag.Tasks[succ.TargetTaskID]
			succNode.Predecessors = append(succNode.Predecessors, id)
			succNode.RemainingDependencies++
		}
	}

	return dag, nil
}
