package parser

import (
	"testing"
)

func TestParseValidDAG(t *testing.T) {
	validJSON := `{
		"dag_id": "dag1",
		"arrival_time": 1716682000,
		"tasks": [
			{
				"task_id": "task-0",
				"base_computation": 120,
				"successors": [
					{ "task_id": "task-1", "data_size": 450 },
					{ "task_id": "task-2", "data_size": 800 }
				]
			},
			{
				"task_id": "task-1",
				"base_computation": 85,
				"successors": [
					{ "task_id": "task-3", "data_size": 300 }
				]
			},
			{
				"task_id": "task-2",
				"base_computation": 200,
				"successors": [
					{ "task_id": "task-3", "data_size": 600 }
				]
			},
			{
				"task_id": "task-3",
				"base_computation": 50,
				"successors": []
			}
		]
	}`

	dag, err := Parse([]byte(validJSON))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if dag.ID != "dag1" {
		t.Errorf("expected dag_id 'dag1', got %s", dag.ID)
	}

	expectedTasks := []string{"dag1:task-0", "dag1:task-1", "dag1:task-2", "dag1:task-3"}
	for _, expected := range expectedTasks {
		if _, exists := dag.Tasks[expected]; !exists {
			t.Errorf("missing expected task %s", expected)
		}
	}

	// Verify predecessor links and indegrees
	task3 := dag.Tasks["dag1:task-3"]
	if task3.RemainingDependencies != 2 {
		t.Errorf("expected task-3 remaining dependencies to be 2, got %d", task3.RemainingDependencies)
	}
	if len(task3.Predecessors) != 2 {
		t.Errorf("expected task-3 predecessors to have 2 entries, got %d", len(task3.Predecessors))
	}
}

func TestParseCyclicDAG(t *testing.T) {
	cyclicJSON := `{
		"dag_id": "dag2",
		"arrival_time": 1716682000,
		"tasks": [
			{
				"task_id": "task-0",
				"base_computation": 100,
				"successors": [
					{ "task_id": "task-1", "data_size": 100 }
				]
			},
			{
				"task_id": "task-1",
				"base_computation": 100,
				"successors": [
					{ "task_id": "task-0", "data_size": 100 }
				]
			}
		]
	}`

	_, err := Parse([]byte(cyclicJSON))
	if err == nil {
		t.Fatal("expected cycle detection error, got none")
	}
}

func TestParseInvalidComputation(t *testing.T) {
	invalidJSON := `{
		"dag_id": "dag3",
		"tasks": [
			{
				"task_id": "task-0",
				"base_computation": 0,
				"successors": []
			}
		]
	}`

	_, err := Parse([]byte(invalidJSON))
	if err == nil {
		t.Fatal("expected base_computation validation error, got none")
	}
}
