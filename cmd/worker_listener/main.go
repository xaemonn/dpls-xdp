// cmd/worker_listener/main.go
//
// Worker Listener Daemon — runs on Node B and Node C
//
// This is the "other end" of the real network benchmark.
// The DPLS scheduler on Node A sends UDP trigger packets here.
// This daemon echoes every packet back as an ACK so Node A can measure
// the real round-trip time (RTT) across the AWS VPC network.
//
// In eBPF mode:   Node A's TC hook intercepts the outbound packet before
//                 iptables, rewriting destination based on vault_map.
// In mock mode:   The packet travels through the full Linux kernel IP stack
//                 (netfilter, conntrack, routing table lookup) before leaving.
//
// The RTT difference between the two modes is the measurable proof of the
// kernel-bypass latency improvement.
//
// Usage (run on Node B and C):
//   go build -o worker_listener ./cmd/worker_listener/
//   sudo ./worker_listener
package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

const listenPort = ":9000"

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	hostname, _ := os.Hostname()
	addr, err := net.ResolveUDPAddr("udp4", listenPort)
	if err != nil {
		log.Fatalf("[Worker Listener] Failed to resolve addr: %v", err)
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		log.Fatalf("[Worker Listener] Failed to listen on UDP %s: %v", listenPort, err)
	}
	defer conn.Close()

	log.Printf("[Worker Listener] ★ Running on host=%s | Listening on UDP %s", hostname, listenPort)
	log.Printf("[Worker Listener] Waiting for task trigger packets from DPLS Scheduler on Node A...")

	// Handle graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("[Worker Listener] Shutting down.")
		conn.Close()
		os.Exit(0)
	}()

	buf := make([]byte, 512)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				continue
			}
			log.Printf("[Worker Listener] Read error: %v", err)
			return
		}

		// Decode task ID from 4-byte little-endian payload
		taskID := uint32(0)
		if n >= 4 {
			taskID = binary.LittleEndian.Uint32(buf[:4])
		}

		log.Printf("[Worker Listener] ← Received Task=%d trigger from %s | Sending ACK →",
			taskID, remoteAddr)

		// Echo the exact payload back as ACK
		// Node A measures time from send to this ACK receipt = real network RTT
		ackPayload := fmt.Sprintf("ACK:task=%d", taskID)
		_, werr := conn.WriteToUDP([]byte(ackPayload), remoteAddr)
		if werr != nil {
			log.Printf("[Worker Listener] Failed to send ACK to %s: %v", remoteAddr, werr)
		}
	}
}
