package ebpf

import (
	"testing"

	"dpls-xdp/pkg/api"
)

func TestIPToUint32Packing(t *testing.T) {
	tests := []struct {
		ip       string
		expected uint32
	}{
		{"127.0.0.1", 16777343}, // little endian representation: 127 | (0<<8) | (0<<16) | (1<<24) = 16777343
		{"192.168.1.1", 16885952},
		{"invalid", 0},
	}

	for _, tc := range tests {
		res := IPToUint32(tc.ip)
		if res != tc.expected {
			t.Errorf("for IP %s expected uint32 %d, got %d", tc.ip, tc.expected, res)
		}
	}
}

func TestWriteDependencyRuleMock(t *testing.T) {
	rule := api.DependencyRule{
		SubtaskID:    10,
		RefCount:     2,
		Destinations: []string{"127.0.0.1", "127.0.0.2"},
	}

	err := WriteDependencyRuleToKernel(rule)
	if err != nil {
		t.Fatalf("failed to write rule in mock mode: %v", err)
	}

	// Retrieve mock rule (using helper defined in loader.go stub)
	fetched, exists := GetMockRule(10)
	if !exists {
		t.Fatal("expected mock rule for subtask 10 to exist")
	}
	if fetched.RefCount != 2 {
		t.Errorf("expected RefCount 2, got %d", fetched.RefCount)
	}
}
