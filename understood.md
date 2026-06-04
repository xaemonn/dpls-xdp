# Project Understanding: Dependency-Aware eBPF Data Plane for MEC

Based on a comprehensive review of the `dpls-xdp` repository blueprint, the provided research papers (DPLS and CachOf), and the detailed AI chat transcripts, here is the synthesis of the project and our specific implementation tasks.

## 1. The Core Problem (The "Abstraction Gap")
Mobile Edge Computing (MEC) applications are often structured as **Directed Acyclic Graphs (DAGs)**, where a sequence of subtasks must execute across multiple edge servers. The current state-of-the-art scheduler, **DPLS** (Dynamic Priority List Scheduling from Liu et al.), efficiently assigns these tasks to edge nodes to minimize overall completion time. 

However, DPLS—and similar algorithms like CachOf—rely on a mathematically perfect simulation of the network: `Transmission Time = Data Size / Network Rate`. They ignore the reality of a Linux/Kubernetes network stack, where every data transfer must traverse user-space memory copies, the TCP/IP stack, iptables, and container networking interfaces. This adds hundreds of microseconds to milliseconds of latency per hop, completely destroying the theoretical gains of the scheduler.

## 2. Our Novel Solution: "Brain and Muscle" Architecture
Our project is a systems-engineering breakthrough that bridges application-aware DAG scheduling with kernel-bypass networking. We are taking the DPLS algorithm and hardwiring it directly into the Linux kernel using **eBPF (Extended Berkeley Packet Filter)**.

- **The Brain (DPLS Scheduler in Go):** Analyzes the DAG topology, computes task priorities, and assigns them to edge nodes. 
- **The Muscle (eBPF Data Plane in C):** A set of eBPF programs running inside the kernel that intercepts data packets at the lowest level, bypassing standard TCP/IP networking to instantly route task outputs.
- **The Vault (eBPF Maps):** Kernel-level shared memory (like `BPF_MAP_TYPE_LRU_HASH`) used to retain subtask outputs just long enough to serve all downstream consumers (Fan-Out Dependency Retention), distinguishing our work from historical-based caching (CachOf).
- **The Channeler (Control Plane Bridge):** The Go interface that listens to the Brain and pre-programs the Vault before tasks even execute.

## 3. Our Actual Task
According to the project breakdown, the development of the core DPLS Go scheduler (The Brain) is being handled by a teammate. 

**My specific responsibility is to build the Cross-Layer integration (Steps 2 and 3).**

### Task 1: The Channeler (Userspace Go Bridge)
I need to write the Go code that acts as the interface between the DPLS scheduler and the kernel.
- It will receive a defined `DependencyRule` struct (specifying the Task ID, destination IPs, and consumer count).
- It will use the `cilium/ebpf` library (`bpf2go`) to write these dependency rules directly into the eBPF maps before the associated worker task begins execution.

### Task 2: The eBPF Program (Kernel Data Plane in C)
I need to write the actual eBPF C programs that act as the "Muscle".
- Because we want to entirely bypass the network stack, we will likely attach to the **TC (Traffic Control)** hook rather than `sk_msg` (which still traverses TCP/IP) or XDP (which lacks necessary checksum helpers and adds extreme complexity).
- The program will intercept outgoing worker packets, look up the active rules in the eBPF map, duplicate the payload if there are multiple downstream consumers (Fan-Out), and route the packets directly to the destination nodes.
- It will implement atomic reference counting to handle kernel-level garbage collection (freeing the payload only after the final consumer has received it).

## Next Steps for Development
1. Set up the local environment with Go 1.22, LLVM/Clang (for compiling eBPF), and the `bpf2go` toolchain.
2. Define the exact eBPF Map schemas in C.
3. Write the skeleton Go loader to load the compiled ELF binaries into the kernel.
4. Begin implementing the TC hook logic.
