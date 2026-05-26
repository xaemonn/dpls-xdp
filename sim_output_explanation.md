# DPLS Engine & eBPF Bridge: Real Simulation Output Analysis

This document provides a step-by-step systems-level explanation of the DPLS execution logs generated during the real eBPF kernel run on Linux.

---

## 1. eBPF Map Setup & Initialization
```text
[BPF Setup] Loading eBPF maps and filters...
[eBPF Loader] Successfully loaded BPF maps into kernel memory
[eBPF Loader] Attaching TC BPF program to interface: lo (Simulated attachment via tc filter)
```
* **Systems Flow**: The Go control plane loader (`loader_linux.go`) executed a `bpf()` system call to instruct the Linux kernel to instantiate two BPF maps:
  1. `vault_map` (`BPF_MAP_TYPE_HASH`): Key = `__u32` (TaskID), Value = `struct dependency_rule` (RefCount, Destination IPs).
  2. `retention_map` (`BPF_MAP_TYPE_LRU_HASH`): Holds cached packet payloads.
* **Attachment**: The TC ingress BPF filter is loaded and prepared for loopback (`lo`) network interface traffic.

---

## 2. Task 0: The "Golden Rule" Handoff
```text
[Scheduler Dispatch] Assigning Task dag-5g-pipeline:task-0 to Worker edge-node-1 (IP: 127.0.0.1)
[eBPF Real Bridge] Successfully wrote DependencyRule to Kernel vault_map: {SubtaskID:0 RefCount:2 DestIPs:[16777343 16777343 0 0]}
[Worker Sim edge-node-1] Blasted UDP trigger packet for task dag-5g-pipeline:task-0 (NumericID: 0) to 127.0.0.1:9000
```
* **Step 1 (Userspace Decision)**: The DPLS Dynamic Priority scheduler selects Task 0 (entry node) and schedules it to `edge-node-1` (`127.0.0.1`).
* **Step 2 (The Golden Rule Handoff)**: Right **before** the task starts executing, the Go scheduler pre-programs the kernel. It writes to `vault_map`:
  * Key = `0` (SubtaskID)
  * Value = `RefCount: 2` (both successor tasks, Task 1 and Task 2, require this task's output data)
  * `DestIPs: [16777343, 16777343, 0, 0]`. Note: `16777343` in hex is `0x0100007F`, which represents **`127.0.0.1`** packed as a little-endian IPv4 address in kernel memory.
* **Step 3 (Dataplane Trigger)**: Worker 1 completes its simulated CPU execution duration (`time.Sleep`) and writes a 4-byte little-endian payload containing `0` (Task ID) into a local UDP socket bound to port `9000`. The loopback-attached TC filter intercepts the packet, reads `0`, queries the BPF map, and retains the payload in memory.

---

## 3. Parallel Execution (Tasks 1 & 2)
```text
[Scheduler Event] Task Completed: dag-5g-pipeline:task-0 by Worker edge-node-1
[Scheduler Dispatch] Assigning Task dag-5g-pipeline:task-2 to Worker edge-node-1 (IP: 127.0.0.1)
[eBPF Real Bridge] Successfully wrote DependencyRule to Kernel vault_map: {SubtaskID:2 RefCount:1 DestIPs:[16777343 0 0 0]}
[Scheduler Dispatch] Assigning Task dag-5g-pipeline:task-1 to Worker edge-node-2 (IP: 127.0.0.2)
[eBPF Real Bridge] Successfully wrote DependencyRule to Kernel vault_map: {SubtaskID:1 RefCount:1 DestIPs:[16777343 0 0 0]}
```
* **State Resolution**: Task 0 completes and decrements the remaining dependencies of its successors (Task 1 and Task 2) to 0, transitioning them to `READY`.
* **Dynamic Priorities**: Since two tasks are ready and workers are available, the scheduler dispatches both in parallel:
  * **Task 2** is assigned to `edge-node-1` (`127.0.0.1`). BPF map rule is written: `SubtaskID: 2`, `RefCount: 1` (Task 3 needs it), IP = `127.0.0.1`.
  * **Task 1** is assigned to `edge-node-2` (`127.0.0.2`). BPF map rule is written: `SubtaskID: 1`, `RefCount: 1` (Task 3 needs it), IP = `127.0.0.1`.
* **Execution**: Both execute concurrently.

---

## 4. Exit Node Execution & Complete (Task 3)
```text
[Worker Sim edge-node-2] Blasted UDP trigger packet for task dag-5g-pipeline:task-1 (NumericID: 1) to 127.0.0.1:9000
[Worker Sim edge-node-1] Blasted UDP trigger packet for task dag-5g-pipeline:task-2 (NumericID: 2) to 127.0.0.1:9000
[Scheduler Dispatch] Assigning Task dag-5g-pipeline:task-3 to Worker edge-node-1 (IP: 127.0.0.1)
[eBPF Real Bridge] Successfully wrote DependencyRule to Kernel vault_map: {SubtaskID:3 RefCount:0 DestIPs:[0 0 0 0]}
[Worker Sim edge-node-1] Blasted UDP trigger packet for task dag-5g-pipeline:task-3 (NumericID: 3) to 127.0.0.1:9000
```
* Once both Task 1 and Task 2 finish execution and blast their UDP trigger packets to port 9000, Task 3's remaining dependencies resolve to 0.
* Task 3 is dispatched to `edge-node-1`. Since it has no successors, its `RefCount` is `0`, and destinations are empty. It completes and terminates the pipeline.

---

## 5. Cleanup
```text
[Monitor] All tasks finished execution. Completing pipeline.
==================================================
DAG Execution Finished: State = COMPLETED
Total Pipeline Makespan: 401.134822ms
==================================================
[eBPF Loader] Successfully detached and closed BPF maps
```
* The scheduler monitor thread detects DAG completion and breaks the event loop.
* It prints the final makespan metrics (401.13ms) and detaches BPF filters from the loopback interface, closing kernel map file descriptors cleanly.
