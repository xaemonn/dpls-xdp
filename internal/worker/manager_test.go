package worker

import (
	"testing"

	"dpls-xdp/pkg/api"
)

func TestWorkerManager(t *testing.T) {
	mgr := NewManager()

	w := &api.Worker{
		ID:                "w1",
		IP:                "192.168.1.10",
		ComputeMultiplier: 1.5,
		NetworkBandwidth:  100,
	}

	err := mgr.RegisterWorker(w)
	if err != nil {
		t.Fatalf("failed to register worker: %v", err)
	}

	// Try duplicate registration
	err = mgr.RegisterWorker(w)
	if err == nil {
		t.Fatal("expected error registering duplicate worker, got none")
	}

	// Fetch worker
	fetched, exists := mgr.GetWorker("w1")
	if !exists {
		t.Fatal("expected worker w1 to exist")
	}
	if fetched.IP != "192.168.1.10" {
		t.Errorf("expected IP 192.168.1.10, got %s", fetched.IP)
	}

	// List workers
	list := mgr.ListWorkers()
	if len(list) != 1 {
		t.Errorf("expected 1 worker, got %d", len(list))
	}
}
