//go:build linux
// +build linux

package scheduler

// dial_linux.go previously overrode mockableDial with a real net.Dial for Linux.
// This is now replaced by sendAndWaitForAck() in core.go which uses net.DialUDP
// directly for the real network RTT benchmark. This file is kept as a placeholder.
