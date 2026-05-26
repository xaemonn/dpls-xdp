package scheduler

import (
	"dpls-xdp/pkg/api"
)

// PriorityQueue implements heap.Interface and holds TaskNode pointers.
type PriorityQueue []*api.TaskNode

// Len returns size of queue.
func (pq PriorityQueue) Len() int {
	return len(pq)
}

// Less sorts by DynamicPriority descending (Max-Heap).
func (pq PriorityQueue) Less(i, j int) bool {
	return pq[i].DynamicPriority > pq[j].DynamicPriority
}

// Swap exchanges elements and updates their heap index tracker.
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].QueueIndex = i
	pq[j].QueueIndex = j
}

// Push adds task element to queue.
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*api.TaskNode)
	item.QueueIndex = n
	*pq = append(*pq, item)
}

// Pop removes and returns highest priority task element.
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.QueueIndex = -1
	*pq = old[0 : n-1]
	return item
}
