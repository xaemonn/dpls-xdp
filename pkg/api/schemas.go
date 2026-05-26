package api

import (
	"time"
)

// TaskState represents the operational status of a task.
type TaskState string

const (
	StateWaiting   TaskState = "WAITING"
	StateReady     TaskState = "READY"
	StateRunning   TaskState = "RUNNING"
	StateCompleted TaskState = "COMPLETED"
	StateFailed    TaskState = "FAILED"
)

// Dependency represents a directed edge to a successor task.
type Dependency struct {
	TargetTaskID string `json:"task_id"`
	DataSize     int64  `json:"data_size"` // Bytes
}

// TaskNode represents a task inside a DAG.
type TaskNode struct {
	ID                    string       `json:"task_id"` // Format: "dag_id:task_id"
	BaseComputation       int64        `json:"base_computation"`
	Successors            []Dependency `json:"successors"`
	Predecessors          []string     `json:"-"` // Predecessor IDs populated during Graph registration
	AssignedNodeIP        string       `json:"-"` // IP address where the task is assigned to run

	// Scheduler runtime variables
	State                 TaskState    `json:"-"`
	StaticRankU           float64      `json:"-"`
	StaticRankD           float64      `json:"-"`
	DynamicPriority       float64      `json:"-"`
	RemainingDependencies int32        `json:"-"`
	ReadyAt               time.Time    `json:"-"`
	QueueIndex            int          `json:"-"` // Maintained by container/heap interface
}

// DAG represents a single pipeline submission.
type DAG struct {
	ID          string               `json:"dag_id"`
	ArrivalTime time.Time            `json:"arrival_time"`
	Tasks       map[string]*TaskNode `json:"tasks"`
	State       string               `json:"-"`
}

// Worker represents a heterogeneous compute worker.
type Worker struct {
	ID                string  `json:"worker_id"`
	IP                string  `json:"ip"`
	ComputeMultiplier float64 `json:"compute_multiplier"`
	NetworkBandwidth  float64 `json:"network_bandwidth"` // MB/s
	IsIdle            bool    `json:"-"`
	ActiveTaskID      string  `json:"-"`
}

// DependencyRule represents the contract between Go scheduler and eBPF kernel program.
type DependencyRule struct {
	SubtaskID    uint32   // The numeric ID of the task that is ABOUT to run
	RefCount     uint32   // How many successor tasks need its output? (Fan-out)
	Destinations []string // The IP addresses of the edge nodes running the successors
}
