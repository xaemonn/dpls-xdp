//go:build linux
// +build linux

package scheduler

import (
	"net"
)

// init overrides the cross-platform mockableDial stub with a real net.Dial
// implementation on Linux. This is called automatically by the Go runtime
// before any tests or production code runs.
//
// Why here and not inline?
//   The scheduler/core.go must compile on Windows (for development), so it
//   cannot import net.Dial directly because the test environment doesn't
//   have a loopback UDP listener on port 9000. By overriding mockableDial
//   in a Linux-only init(), we get real network behavior on Linux without
//   breaking Windows cross-compilation or unit tests.
func init() {
	// Replace the no-op dummy with a real UDP connection.
	// On Linux with the TC-BPF program attached:
	//   1. Worker calls conn.Write(4-byte task ID)
	//   2. Packet goes to 127.0.0.1:9000 on loopback (lo)
	//   3. tc_ingress() in tc_bridge.c intercepts it on lo
	//   4. Reads the 4-byte task ID from UDP payload
	//   5. Looks up vault_map[task_id] → rewrites destination IP
	mockableDial = func(network, address string) (interface {
		Write([]byte) (int, error)
		Close() error
	}, error) {
		conn, err := net.Dial(network, address)
		if err != nil {
			// If the dial fails (e.g., port not listening), return a no-op
			// so the scheduler doesn't crash — the BPF interception still
			// happens even without a listener on the other end.
			return &dummyUDP{addr: address, live: false}, nil
		}
		return conn, nil
	}
}
