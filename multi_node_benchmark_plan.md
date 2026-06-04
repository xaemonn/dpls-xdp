# Multi-Node Benchmark Execution Plan
**Goal:** Prove the latency reduction of the DPLS + TC-BPF architecture across a real 3-node network by bypassing standard Linux network stacks (`iptables`, `kube-proxy`, etc.).

---

## 1. Prerequisites (The Cluster)
As outlined in your `system_setup.md`, you need all three Ubuntu VMs running and connected.
1. **VM 1 (Control Plane / Node 0):** Runs the DPLS Go Scheduler (`dpls-scheduler`).
2. **VM 2 (Worker 1):** Executes Subtask 1.
3. **VM 3 (Worker 2):** Executes Subtask 2.

Ensure all three VMs can ping each other via their static IPs (e.g., `192.168.56.10`, `11`, `12`).
Ensure you know the name of the network interface connecting them (usually `eth0`, `ens33`, or `enp0s3`). Run `ip addr` on the VMs to find this.

## 2. The Benchmark DAG Scenario
To prove kernel bypass, you need a task dependency that forces data to cross the physical network.
**Scenario:** `Task A` (runs on VM 2) → feeds data to → `Task B` (runs on VM 3).

*In `main.go`, we will hardcode the DAG so `Task A` is forced to assign to Worker 1, and `Task B` is forced to assign to Worker 2.*

---

## 3. Phase 1: The Baseline Run (No eBPF)
In this phase, we rely on standard Linux TCP/IP routing and Kubernetes/K3s networking.

### What physically happens:
1. Task A finishes on VM 2.
2. The Go worker sends the result payload.
3. The packet travels down VM 2's network stack: Socket buffer → TCP/UDP layer → IP routing tables → `iptables` rules (K3s overhead) → NIC driver.
4. Travels over the wire to VM 3.
5. Travels up VM 3's network stack: NIC driver → `iptables` → IP routing → TCP/UDP layer → Socket buffer → Go worker.

### Execution:
On VM 1 (Control Plane):
```bash
# Run the scheduler in mock mode (no eBPF)
go run main.go --mode mock
```
*Run this 10 times and record the average DAG completion time.*

---

## 4. Phase 2: The eBPF Kernel-Bypass Run
In this phase, we attach our TC-BPF programs to the network interfaces of VM 2 and VM 3.

### What physically happens:
1. Task A finishes on VM 2.
2. The Go worker sends a lightweight UDP "trigger" packet.
3. **VM 2 EGRESS BYPASS:** The packet hits the TC-BPF hook *immediately* before leaving the NIC. The `dpls_tc_egress` program reads the `vault_map`, identifies the packet belongs to Task A, and instantly rewrites the destination IP to VM 3. It skips the massive `iptables` and routing table traversal.
4. Travels over the wire to VM 3.
5. **VM 3 INGRESS BYPASS:** The packet arrives at VM 3's NIC. The `dpls_tc_ingress` program intercepts it *immediately*, reads the `vault_map`, and passes it directly to the worker application socket, completely skipping VM 3's incoming network stack.

### Execution:
On VM 1 (Control Plane):
```bash
# Tell the scheduler to attach eBPF to the physical interface (e.g., eth0 or ens33)
sudo go run main.go --mode ebpf --interface eth0 --ebpf-elf internal/ebpf/c/tc_bridge.o
```
*Run this 10 times and record the average DAG completion time.*

---

## 5. Expected Results & Thesis Defense
Because you are now crossing physical network boundaries, the overhead of standard Linux networking becomes a real bottleneck (usually adding 0.5ms to 2ms of delay per hop, especially with Kubernetes proxy rules). 

By rewriting the packets at the TC (Traffic Control) layer using eBPF, you will cut out the OS networking overhead.
You should expect to see the **eBPF Run complete the DAG noticeably faster** than the Baseline Run. 

**This Delta (Baseline Time - eBPF Time) is your exact Proof of Work.** It proves that your "Dependency-Aware Proactive Routing" mathematically outperforms reactive TCP/IP routing in Edge Computing environments.
