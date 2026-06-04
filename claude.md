# Project Brief — eBPF-Accelerated Dependency-Aware Task Offloading in Mobile Edge Computing
### B.Tech Research Project — Complete Reference Document
#### Prepared: June 2026

---

## 1. Project Title (Tentative)

"Dependency-Driven Kernel-Bypass Scheduling for Multi-DAG Task Offloading in
Mobile Edge Computing Using Extended Berkeley Packet Filter"

---

## 2. The Problem We Are Solving

Modern applications like augmented reality, autonomous driving, and real-time
healthcare analytics are too heavy for a single device to handle locally. Mobile
Edge Computing (MEC) solves this by offloading computation to small edge servers
placed physically close to the user.

These applications are modeled as Directed Acyclic Graphs (DAGs) — chains of
dependent subtasks where each subtask must wait for its predecessor to finish
before it can begin. The research challenge is: given limited edge server
resources and multiple DAG applications arriving unpredictably, how do you
schedule subtasks across edge servers to minimize total completion time?

The existing best algorithm for this — DPLS from Liu et al. 2026 — works well
in simulation. But simulation makes one critical assumption that has never been
validated on real hardware:

> Transmission Time = Data Size / Network Rate

This equation treats the network as a frictionless pipe. In reality, when a
subtask finishes on Node 1 and its output needs to reach Node 2, the data must
traverse:
- User space to kernel space memory copy
- Full Linux TCP/IP protocol stack
- iptables firewall rules (Kubernetes manages these automatically)
- Container network interface processing
- The same stack in reverse on Node 2

This overhead adds hundreds of microseconds to milliseconds per subtask
transfer — and accumulates across every edge in a DAG with 10 to 30 subtasks.
No prior paper has measured or fixed this.

---

## 3. Papers Studied and Key Insights

### Paper 1 — Base Paper (Liu et al., IEEE Internet of Things Journal, 2026)

**Full title:** Dependency-Aware Dynamic Priority Scheduling for Online
Multi-DAG Task Offloading in Mobile Edge Computing

**What it does:**
- Formulates the multi-DAG scheduling problem as an Integer Linear Program
- Proves the problem is NP-hard
- Proposes the Dynamic Priority List Scheduling (DPLS) algorithm
- DPLS assigns priority to each subtask based on:
  - Upward rank (impact on downstream tasks)
  - Downward rank (time already consumed before this subtask)
  - Subtask criticality (is it on the longest path?)
  - Task volume (total computational weight)
  - Contention level (how long has it been waiting?)
- Critical subtasks go to the server that can finish them earliest
- Non-critical subtasks go to the server that becomes free earliest
- Reduces mean task completion time by approximately 15% vs existing algorithms
- Validated entirely through Python simulation — never on real hardware

**The critical gap:**
Section 3C models transmission time as data size divided by network rate.
All kernel overhead is assumed away. This has never been tested on a real cluster.

---

### Paper 2 — CachOf (Zhao et al., IEEE Transactions on Computers, May 2025)

**Full title:** Dynamic Caching Dependency-Aware Task Offloading in
Mobile Edge Computing

**What it does:**
- Extends DAG scheduling with edge content caching
- Watches which subtask outputs get requested repeatedly over time
- Ranks outputs by popularity using historical request frequency
- Uses a 0-1 knapsack algorithm to decide what to cache given limited storage
- Combines caching with a Deep Deterministic Policy Gradient (DDPG)
  reinforcement learning algorithm for offloading decisions
- Reduces latency compared to non-caching baselines

**The critical gap:**
- Caching decision is based on historical popularity — not the current DAG structure
- Cached results still travel through the full Linux networking stack when served
- Runs entirely in Python simulation — never on real hardware
- Makes the same frictionless network assumption as Paper 1

**Why this paper matters for our novelty:**
CachOf already combines dependency-aware scheduling with caching. This means
we cannot claim "adding caching to DAG scheduling" as novel. However, our
caching mechanism is fundamentally different — see Section 5.

---

### Paper Group 3 — Extended Berkeley Packet Filter / Express Data Path Papers

**Navarro et al. (2021) — In-Kernel 5G User Plane Function using Express Data Path**
- Built a 5G User Plane Function entirely inside the Linux kernel
- Achieved 10 million packets per second using 60% of 6 CPU cores
- Key limitation: treats all packets identically, no application awareness

**Cable / Zhou et al. (2023) — Express Data Path in Free5GC**
- Integrated Express Data Path into the Free5GC 5G core
- Reduced per-packet processing delay by over 30%
- Improved system throughput by approximately 25%
- Key limitation: focuses on load balancing sessions, ignores DAG dependencies

**Leiter et al. (2025) — eBPF vs iptables in Kubernetes**
- Compared Cilium (Extended Berkeley Packet Filter) against traditional
  iptables/kube-proxy in Kubernetes under network slicing load
- Found comparable throughput but dramatically lower jitter with eBPF
- Key limitation: generic Container Network Interface optimization,
  no awareness of application-level DAG structure

**Moreira et al. (2026) — Intelligent User Plane Function for B5G**
- Uses Extended Berkeley Packet Filter to timestamp GTP-U tunnels
- Drives a Deep Q-Network based path selector
- Key limitation: reactive telemetry — waits to observe, then reacts

**The critical gap across all Extended Berkeley Packet Filter papers:**
None of these papers know or care about DAG scheduling. They treat every
packet identically. No concept of "this packet is the output of Subtask A
and must reach Subtask B urgently before the next DAG step can begin."

---

## 4. The Gap — What Nobody Has Done

Two completely separate worlds exist in the literature:

| World | Papers | What they do | What they ignore |
|-------|--------|--------------|------------------|
| DAG Scheduling | Liu 2026, Zhao 2025 | Optimize task assignment | Assume frictionless network |
| Kernel Networking | Navarro, Cable, Leiter | Speed up packet forwarding | Ignore application dependencies |

**No paper has ever connected these two worlds.**

The gap is confirmed by web search across IEEE, ACM, and arXiv as of May 2026.
No paper integrates a DAG-aware scheduler with an Extended Berkeley Packet Filter
data plane where the scheduler proactively programs kernel routing rules based
on the dependency graph.

---

## 5. Our Contribution — What We Are Building

### The Core Idea

When the DPLS scheduler resolves a dependency edge (Subtask A on Node 1 feeds
Subtask B on Node 2), it immediately writes a routing rule into a shared
Extended Berkeley Packet Filter map in the Linux kernel — before Subtask A
even begins executing.

When Subtask A finishes, its output packet arrives at the network interface.
The Extended Berkeley Packet Filter program intercepts it at the earliest
possible kernel entry point, looks up the pre-written rule, and transfers
the data directly into Subtask B's socket buffer — bypassing:
- The TCP/IP protocol stack
- iptables firewall rules
- Multiple kernel-to-user-space memory copies

This makes the frictionless network assumption of the base paper practically
achievable on real hardware — for the first time.

### How Our Caching Differs From CachOf

| Aspect | CachOf (Zhao 2025) | Our System |
|--------|-------------------|------------|
| Decision source | Historical request popularity | Live DAG dependency graph |
| Timing | Reactive — caches after observing repeats | Proactive — retains before consumer requests |
| Storage location | Application layer storage | Kernel-level LRU hash map |
| Serving a cached result | Still traverses Linux network stack | Bypasses network stack entirely |
| What it answers | What was popular yesterday? | What does the current DAG tell us is needed now? |

**The one sentence distinction:**
CachOf decides what to cache by looking at what was popular yesterday.
Our system decides what to retain by looking at what the DAG graph tells
us will be needed in the next few hundred milliseconds — and does this
retention inside the Linux kernel, not in user space.

### The "Brain and Muscle" Architecture

**The Brain — Control Plane (Go)**
- Implements the DPLS scheduling algorithm
- Resolves DAG dependency edges
- Assigns subtasks to edge nodes via Kubernetes API
- For every dependency edge resolved, writes routing rule into Extended
  Berkeley Packet Filter map via the cilium/ebpf Go library
- For fan-out patterns (one subtask feeding multiple consumers), writes
  multiple entries and a consumer count

**The Muscle — Data Plane (C / Extended Berkeley Packet Filter)**
- Socket operations program: intercepts TCP connection establishment,
  populates the socket map with active container sockets
- Socket message program: intercepts every outgoing data send, looks up
  the routing map, redirects data to destination socket
- LRU hash map: retains outputs for multi-consumer fan-out patterns,
  serves each consumer when they become ready

**The Novel Interface:**
The shared Extended Berkeley Packet Filter map that the Brain writes to
and the Muscle reads from. This bidirectional coupling between the
application-layer scheduler and the kernel data plane does not exist
in any prior work.

---

## 6. Novelty Statement

> "Existing DAG scheduling research assumes a frictionless network abstraction
> and validates only through simulation. Existing Extended Berkeley Packet Filter
> research accelerates generic packet forwarding with no application awareness.
> We present the first system that integrates dependency-aware DAG scheduling
> directly with a kernel-bypass data plane, allowing the scheduler to
> proactively program the kernel with subtask routing rules before data is
> generated, and provide the first empirical measurement of whether the
> frictionless network assumption in prior simulation-based work holds in a
> real edge cluster."

### Why This Is Not "Just Combining Two Papers"

The combination requires a new design element that neither paper provides —
the interface between the scheduler and the kernel. DPLS currently has no
mechanism to communicate its scheduling decisions to anyone. The Extended
Berkeley Packet Filter papers have no concept of DAG-structured routing rules.

Building that interface — deciding its data structure, its timing, its
consistency guarantees — is the engineering contribution. No AI tool or
prior paper tells us how to design it.

---

## 7. Project Objectives

**Objective 1 — Measure the Real Cost of Inter-Subtask Data Transfer**

Implement the DPLS algorithm on a real edge cluster and measure how much
the actual network overhead deviates from what the simulation assumes,
providing the first real-world validation of the base paper.

**Objective 2 — Bypass Kernel Overhead Using Extended Berkeley Packet Filter**

Program the Linux kernel with DAG routing rules before subtask execution
begins, so that when a subtask finishes, its output is transferred directly
to the next subtask's memory — bypassing the TCP/IP stack, iptables, and
memory copies entirely.

**Objective 3 — Retain Subtask Outputs for Multiple Downstream Consumers**

When one subtask feeds multiple downstream subtasks, store its output inside
the kernel and serve each consumer directly when they become ready — decided
by the DAG graph structure, not by historical popularity like CachOf.

---

## 8. Implementation Plan

| Phase | Work | Duration |
|-------|------|----------|
| Phase 1 — Testbed Setup | 3 Ubuntu VMs, K3s cluster, Cilium CNI, verify pod-to-pod connectivity | Week 1-2 |
| Phase 2 — DPLS Scheduler | Implement Algorithms 1, 2, 3 from base paper in Go, assign subtasks via Kubernetes API | Week 3-4 |
| Phase 3 — Baseline Measurement | Run scheduler with standard networking, record DAG completion time, latency, CPU usage | Week 5 |
| Phase 4 — eBPF Data Plane | Write socket operations and socket message programs in C, test bypass on simple transfers | Week 6-7 |
| Phase 5 — Integration | Connect scheduler to kernel via shared map, test end-to-end with real DAGs | Week 8 |
| Phase 6 — Output Retention | Add fan-out detection, LRU kernel map retention, multi-consumer serving | Week 9 |
| Phase 7 — Evaluation and Writing | Three-configuration comparison, plots, paper writeup | Week 10-11 |

**Total: 11 weeks — comfortably within August/September defense window**

---

## 9. Evaluation Strategy

Three configurations will be compared:

**Configuration 1 — Baseline**
DPLS scheduler + standard Kubernetes TCP/IP networking (iptables mode).
Represents what the base paper's algorithm actually performs like on real hardware.

**Configuration 2 — Extended Berkeley Packet Filter Accelerated**
DPLS scheduler + Extended Berkeley Packet Filter kernel bypass.
Removes kernel overhead from subtask transfers.

**Configuration 3 — Extended Berkeley Packet Filter with DAG-Aware Retention**
Configuration 2 + multi-consumer output retention.
Covers fan-out dependency patterns.

**Metrics:**
- End-to-end DAG completion time (varying DAG sizes: 10 to 30 subtasks)
- Per-hop transfer latency (95th and 99th percentile)
- CPU overhead on edge nodes (using Linux perf)
- Latency variance and jitter across repeated runs

**Expected findings:**
- Configuration 1 will show measurable gap from simulation predictions
- Configuration 2 will close most of that gap
- Configuration 3 will further reduce latency in fan-out DAG patterns

---

## 10. Scope Boundaries

**What we are building:**
- Real Go implementation of DPLS (not a simulation)
- Custom Extended Berkeley Packet Filter programs for dependency-aware routing
- Shared Extended Berkeley Packet Filter map connecting scheduler to kernel
- 3-node K3s testbed on Ubuntu virtual machines

**What we are not building:**
- A new scheduling algorithm (we use DPLS from the base paper)
- A full 5G core (K3s cluster serves as edge environment)
- A production-grade system (research prototype)

**The contribution is the integration layer and the empirical evidence,
not a new algorithm.**

---

## 11. Comparison With Prior Work

| Feature | Liu 2026 | Zhao 2025 | Our Work |
|---------|----------|-----------|----------|
| DAG-aware scheduling | Yes | Yes | Yes (DPLS) |
| Dependency-aware caching | No | Yes (popularity) | Yes (DAG-driven) |
| Extended Berkeley Packet Filter data plane | No | No | Yes |
| Real testbed validation | No | No | Yes |
| Kernel-bypass routing | No | No | Yes |
| Frictionless assumption tested | No | No | Yes (first time) |
| Scheduler programs the kernel | No | No | Yes (novel interface) |

---

## 12. Key Technical Facts for Professor Meeting

- The base paper's transmission model (Equation 2, Section 3C):
  tau = d / lambda (data size divided by transmission rate)

- Navarro et al. achieved 10 million packets per second (not 10 Mbps —
  note: Mbps is megabits per second, Mpps is million packets per second)
  using 60% of 6 CPU cores with an in-kernel Express Data Path User Plane
  Function

- Cable / Zhou et al. reduced per-packet UPF processing delay by over 30%
  and improved system throughput by approximately 25%

- Leiter et al. showed Extended Berkeley Packet Filter (Cilium) yields
  comparable throughput to iptables but dramatically lower jitter

- Web search confirmed as of May 2026: no paper combines DAG-aware
  scheduling with Extended Berkeley Packet Filter kernel-bypass data plane

- The DPLS algorithm time complexity is O(N|V|log2(N|V|)) where N is
  the number of DAG tasks and |V| is the maximum number of subtask nodes

---

## 13. Anticipated Professor Questions and Answers

**Q: Is this not just implementing two existing papers together?**

A: The combination requires a new design element that neither paper provides —
the interface between the scheduler and the kernel. DPLS has no mechanism to
communicate scheduling decisions to the kernel. Extended Berkeley Packet Filter
papers have no concept of DAG routing rules. Designing that interface, its data
structure, and its timing is the engineering contribution. Additionally, the
empirical measurement itself is a contribution — the base paper has never been
validated on real hardware.

**Q: Why not improve the scheduling algorithm instead?**

A: The DPLS algorithm is already proven near-optimal by Liu et al. The
bottleneck we are addressing is not algorithmic — it is the mismatch between
what the algorithm assumes the network does and what the network actually does.
Our contribution is making that assumption true, not finding a better algorithm.

**Q: How does this differ from CachOf which also adds caching?**

A: CachOf decides what to cache based on historical popularity — it looks
backward at past requests. Our system decides what to retain based on the
current DAG graph — it looks forward at known future consumers. Additionally,
CachOf's cached results still traverse the Linux networking stack when served.
Our retained outputs are served from kernel memory, bypassing the stack entirely.

**Q: Is Extended Berkeley Packet Filter implementation too complex for a B.Tech project?**

A: With modern AI-assisted coding tools, the implementation timeline is
manageable at 8 to 11 weeks. The hard parts are not coding — they are getting
the Extended Berkeley Packet Filter programs to pass the Linux kernel verifier
and getting clean latency measurements. These are research engineering
challenges appropriate for a B.Tech project.

---

*Document compiled from research discussion — June 2026*
*Base paper: Liu et al., IEEE Internet of Things Journal, Vol. 13, No. 3, February 2026*
*CachOf paper: Zhao et al., IEEE Transactions on Computers, Vol. 74, No. 5, May 2025*
