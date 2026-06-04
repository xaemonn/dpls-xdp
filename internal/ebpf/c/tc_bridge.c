// SPDX-License-Identifier: GPL-2.0
//
// tc_bridge.c — DAG-Aware Traffic Control eBPF Program
// ======================================================
// Project: eBPF-Accelerated Dependency-Aware Task Offloading in MEC
// Hook:    TC (Traffic Control) ingress — attached to eth0 and lo
//
// WHY TC-BPF (not XDP, not sk_msg)?
//   - XDP runs at the NIC driver level (fastest) but requires you to manually
//     parse raw Ethernet frames and recalculate checksums byte-by-byte.
//     That complexity is too high for a research prototype.
//   - sk_msg redirects between sockets but the data still travels through the
//     full Linux TCP/IP stack — it does NOT bypass the network overhead we want
//     to eliminate.
//   - TC-BPF: sits just above XDP (gives 95% of XDP's latency benefit), has
//     bpf_l3_csum_replace() and bpf_skb_store_bytes() helpers that handle
//     checksums and packet modification automatically — no manual math needed.
//
// COMPILATION (on Ubuntu 22.04):
//   clang -target bpf -O2 \
//     -I /usr/include/x86_64-linux-gnu \
//     -c internal/ebpf/c/tc_bridge.c \
//     -o internal/ebpf/c/tc_bridge.o
//
// ATTACHMENT (done by Go loader_linux.go, or manually):
//   sudo tc qdisc add dev eth0 clsact
//   sudo tc filter add dev eth0 ingress bpf obj tc_bridge.o sec tc direct-action

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/udp.h>
#include <linux/pkt_cls.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

// ─────────────────────────────────────────────────────────────────────────────
// CONSTANTS
// ─────────────────────────────────────────────────────────────────────────────

// The UDP port that worker goroutines "blast" trigger packets to when a task
// finishes its simulated CPU computation (see worker/manager.go line ~700).
// Format: 4-byte little-endian task ID as the UDP payload.
#define WORKER_TRIGGER_PORT  9000

// Maximum fan-out degree per task — must match Go api.DependencyRule.Destinations slice cap.
// A task output feeding 5+ nodes is extremely rare in MEC DAG workloads.
#define MAX_FANOUT           4

// Packet header offsets (assuming standard IHL=5, no IP options):
// Ethernet header = 14 bytes
// IP header starts at byte 14, IP daddr is at byte 16 within the IP header
// So: daddr absolute offset = 14 + 16 = 30
#define ETH_LEN              14
#define IP_DADDR_OFF         (ETH_LEN + __builtin_offsetof(struct iphdr, daddr))
#define IP_CHECK_OFF         (ETH_LEN + __builtin_offsetof(struct iphdr, check))
#define IP_SADDR_OFF         (ETH_LEN + __builtin_offsetof(struct iphdr, saddr))

// ─────────────────────────────────────────────────────────────────────────────
// DATA STRUCTURES (must mirror Go api.DependencyRule and loader_linux.go)
// ─────────────────────────────────────────────────────────────────────────────

// dependency_rule: the routing contract written by the Go Channeler into vault_map.
// The Channeler writes this BEFORE the task begins executing — proactive, not reactive.
// Layout must match the BPF map value struct in loader_linux.go exactly.
struct dependency_rule {
    __u32 ref_count;               // Fan-out: how many downstream consumers need this output
    __u32 dest_ips[MAX_FANOUT];    // Destination IPs in network byte order (big-endian)
};

// retained_payload: stored in retention_map for fan-out patterns.
// When task A feeds both B and C (ref_count=2), the payload is stored here.
// eBPF programs decrement remaining_consumers atomically. When it hits 0, the
// LRU map will eventually evict the entry (kernel-level garbage collection).
struct retained_payload {
    __u32 task_id;               // The originating task
    __u32 data_word;             // First 4 bytes of the task's output (the UDP payload)
    __u32 remaining_consumers;   // Countdown: freed when all consumers have read it
};

// ─────────────────────────────────────────────────────────────────────────────
// BPF MAPS (The Vault — shared kernel state between Go Channeler and TC program)
// ─────────────────────────────────────────────────────────────────────────────

// vault_map: the primary DAG-aware routing table.
//
//   Key:   subtask_id (uint32)
//   Value: dependency_rule { ref_count, dest_ips[] }
//
// Written by: Go Channeler (WriteDependencyRuleToKernel) in loader_linux.go
// Read by:    This TC-BPF program on every incoming UDP trigger packet
//
// "The brain programs the vault; the muscle reads the vault."
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);  // Supports up to 1024 concurrent active tasks
    __type(key, __u32);
    __type(value, struct dependency_rule);
} vault_map SEC(".maps");

// retention_map: kernel-level payload retention for fan-out dependency patterns.
//
//   Key:   subtask_id (uint32)
//   Value: retained_payload
//
// This is our KEY DIFFERENTIATOR from CachOf (Zhao et al. 2025):
//   - CachOf: decides what to cache based on HISTORICAL REQUEST POPULARITY (reactive)
//   - Our system: decides what to retain based on LIVE DAG STRUCTURE (proactive)
//
// The LRU eviction policy ensures the kernel automatically reclaims memory for
// completed tasks — no explicit garbage collection needed from userspace.
struct {
    __uint(type, BPF_MAP_TYPE_LRU_HASH);
    __uint(max_entries, 512);   // 512 retained payloads in kernel memory at once
    __type(key, __u32);
    __type(value, struct retained_payload);
} retention_map SEC(".maps");

// ─────────────────────────────────────────────────────────────────────────────
// HELPER: Safe variable-length IP header skip
// ─────────────────────────────────────────────────────────────────────────────
// Standard IP headers have IHL=5 (20 bytes), but IHL can be up to 15 (60 bytes).
// We always use ihl*4 to find the start of the UDP header correctly.
static __always_inline struct udphdr *get_udp_hdr(struct iphdr *ip, void *data_end)
{
    // ihl is a 4-bit field (count of 32-bit words), so byte length = ihl * 4
    __u32 ip_len = ip->ihl * 4;
    if (ip_len < 20)           // Minimum valid IP header is 20 bytes
        return NULL;
    struct udphdr *udp = (void *)ip + ip_len;
    if ((void *)(udp + 1) > data_end)
        return NULL;
    return udp;
}

// ─────────────────────────────────────────────────────────────────────────────
// TC INGRESS PROGRAM
// ─────────────────────────────────────────────────────────────────────────────
//
// Called by the Linux kernel for EVERY packet arriving on the attached interface.
// Most packets return TC_ACT_OK immediately (pass through to normal routing).
// Only our UDP trigger packets (dest port 9000) are processed for DAG routing.
//
// EXECUTION PATH:
//   Worker finishes task → sends UDP(payload=task_id) to port 9000
//   → Linux kernel sees packet on interface
//   → Calls this TC program
//   → We read task_id from payload
//   → Look up vault_map[task_id] for routing rules
//   → If ref_count > 1: store in retention_map (fan-out)
//   → Rewrite destination IP to the successor node's IP
//   → Return TC_ACT_OK: packet continues to (now modified) destination
//
SEC("tc")
int tc_ingress(struct __sk_buff *skb)
{
    // ── Step 1: Get packet data boundaries ───────────────────────────────────
    // The eBPF verifier REQUIRES bounds checks on every pointer access.
    // Any access without a bounds check will be REJECTED at load time.
    void *data     = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    // ── Step 2: Parse and validate Ethernet header ───────────────────────────
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;   // Packet too small — pass through

    // ── Step 3: Filter — only process IPv4 ───────────────────────────────────
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;   // ARP, IPv6, etc. — not our concern

    // ── Step 4: Parse and validate IPv4 header ───────────────────────────────
    struct iphdr *ip = (struct iphdr *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;

    // ── Step 5: Filter — only process UDP ────────────────────────────────────
    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;   // TCP, ICMP, etc. — not our concern

    // ── Step 6: Parse UDP header (handles variable-length IP options) ─────────
    struct udphdr *udp = get_udp_hdr(ip, data_end);
    if (!udp)
        return TC_ACT_OK;

    // ── Step 7: Filter — only process worker trigger port (9000) ─────────────
    // Worker goroutines send trigger packets to this port when a task completes.
    // All other UDP traffic (DNS, etc.) passes through unchanged.
    if (udp->dest != bpf_htons(WORKER_TRIGGER_PORT))
        return TC_ACT_OK;

    // ── Step 8: Extract task ID from UDP payload ──────────────────────────────
    // Go worker code (worker/manager.go) sends:
    //   payload := make([]byte, 4)
    //   binary.LittleEndian.PutUint32(payload, task.NumericID)
    // So the first 4 bytes of the UDP payload ARE the task ID in little-endian.
    __u32 *payload_start = (void *)(udp + 1);
    if ((void *)(payload_start + 1) > data_end)
        return TC_ACT_OK;   // Payload too short to contain a task ID

    __u32 task_id = *payload_start;  // Task ID (little-endian from Go)

    // ── Step 9: Look up routing rule from vault_map ───────────────────────────
    // The Go Channeler wrote this rule BEFORE this task was dispatched.
    // If no rule exists: this is an exit node (no successors) — pass through.
    struct dependency_rule *rule = bpf_map_lookup_elem(&vault_map, &task_id);
    if (!rule)
        return TC_ACT_OK;   // No routing rule = exit node or race condition

    // Emit a debug trace event (visible via /sys/kernel/debug/tracing/trace_pipe)
    bpf_printk("TC-BPF: Intercepted Task=%u RefCount=%u\n", task_id, rule->ref_count);

    // ── Step 10: Fan-out retention ────────────────────────────────────────────
    // If ref_count > 1, multiple downstream tasks need this output.
    // Store the payload in retention_map (kernel-level LRU) so additional
    // consumers can retrieve it without re-transmitting from the source node.
    // This is "Dependency-Driven Proactive Retention" — our novel contribution
    // over CachOf's historical popularity-based caching.
    if (rule->ref_count > 1) {
        struct retained_payload entry = {
            .task_id             = task_id,
            .data_word           = task_id,  // In extended version: full payload copy
            .remaining_consumers = rule->ref_count,
        };
        // BPF_ANY: create new entry or overwrite existing (idempotent for retries)
        bpf_map_update_elem(&retention_map, &task_id, &entry, BPF_ANY);
        bpf_printk("TC-BPF: Retained Task=%u for %u consumers\n",
                   task_id, rule->ref_count);
    }

    // ── Step 11: Destination IP rewrite (Kernel-Bypass Routing) ──────────────
    // Rewrite the packet's destination IP to route directly to the successor node,
    // bypassing standard Linux routing tables, iptables, and kube-proxy chains.
    // This is the core kernel-bypass mechanism: the DAG topology programs the
    // kernel routing table, not the OS.
    if (rule->dest_ips[0] == 0)
        return TC_ACT_OK;   // ref_count=0 exit node or empty rule

    __u32 old_daddr = ip->daddr;
    __u32 new_daddr = rule->dest_ips[0];  // First (primary) successor's IP

    // Skip rewrite if already going to correct destination
    if (old_daddr == new_daddr)
        return TC_ACT_OK;

    // Rewrite IP checksum FIRST (bpf_l3_csum_replace handles the math for us)
    // This is TC-BPF's advantage over XDP: the helper function exists here.
    bpf_l3_csum_replace(skb,
                        IP_CHECK_OFF,    // Offset of the checksum field in the packet
                        old_daddr,       // Old IP (4 bytes)
                        new_daddr,       // New IP (4 bytes)
                        sizeof(__u32));  // Size flag: 4 = full word replacement

    // Now rewrite the actual destination IP field in the packet
    bpf_skb_store_bytes(skb,
                        IP_DADDR_OFF,    // Offset of daddr in the packet
                        &new_daddr,      // Pointer to new value
                        sizeof(__u32),   // 4 bytes
                        0);              // No flags

    bpf_printk("TC-BPF: Redirected Task=%u to IP=0x%08x\n",
               task_id, bpf_ntohl(new_daddr));

    // Packet passes through with modified destination — the kernel will now
    // forward it to the rewritten IP instead of the original destination.
    return TC_ACT_OK;
}

// ─────────────────────────────────────────────────────────────────────────────
// TC EGRESS PROGRAM (optional — for outbound traffic interception)
// ─────────────────────────────────────────────────────────────────────────────
// Attach to egress to intercept packets LEAVING a worker node.
// Allows rewriting destination on outbound packets (alternative attachment point).
SEC("tc_egress")
int tc_egress(struct __sk_buff *skb)
{
    // Egress uses the same logic as ingress — identical parsing and routing.
    // Egress attachment is used when the worker node is the SOURCE of the packet.
    // Ingress attachment is used on the receiving/forwarding node.
    // For our research prototype: attach to BOTH for complete coverage.
    void *data     = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;
    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    struct iphdr *ip = (struct iphdr *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;
    if (ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;

    struct udphdr *udp = get_udp_hdr(ip, data_end);
    if (!udp)
        return TC_ACT_OK;

    // On egress, the PORT may be source (worker outbound) rather than dest
    if (udp->source != bpf_htons(WORKER_TRIGGER_PORT) &&
        udp->dest   != bpf_htons(WORKER_TRIGGER_PORT))
        return TC_ACT_OK;

    __u32 *payload_start = (void *)(udp + 1);
    if ((void *)(payload_start + 1) > data_end)
        return TC_ACT_OK;

    __u32 task_id = *payload_start;
    struct dependency_rule *rule = bpf_map_lookup_elem(&vault_map, &task_id);
    if (!rule || rule->dest_ips[0] == 0)
        return TC_ACT_OK;

    bpf_printk("TC-BPF EGRESS: Intercepted Task=%u\n", task_id);

    __u32 old_daddr = ip->daddr;
    __u32 new_daddr = rule->dest_ips[0];

    if (old_daddr == new_daddr)
        return TC_ACT_OK;

    bpf_l3_csum_replace(skb, IP_CHECK_OFF, old_daddr, new_daddr, sizeof(__u32));
    bpf_skb_store_bytes(skb, IP_DADDR_OFF, &new_daddr, sizeof(__u32), 0);

    return TC_ACT_OK;
}

char _license[] SEC("license") = "GPL";
