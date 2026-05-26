package ebpf

import (
	"net"
)

// IPToUint32 converts an IPv4 address string to little-endian uint32.
func IPToUint32(ipStr string) uint32 {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0
	}
	// Pack into little endian uint32
	return uint32(ipv4[0]) | (uint32(ipv4[1]) << 8) | (uint32(ipv4[2]) << 16) | (uint32(ipv4[3]) << 24)
}
