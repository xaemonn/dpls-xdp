# System Setup Guide: Ubuntu 22.04 Benchmark Environment
## eBPF-Accelerated Dependency-Aware Task Offloading in MEC
### B.Tech Research Project — June 2026

> **Purpose:** Exact, copy-paste setup script for all three teammates. If everyone runs this identical script, the three Ubuntu 22.04 VMs will be perfectly identical benchmark environments — any one can serve as the reference node.  
> **Source:** Gemini AI chat transcript (ubuntu setup session), BTP.pdf, claude.md, and dpls_runtime_blueprint.md.

---

## Why This Specific Setup?

Standard K3s installs `flannel` (for networking) and `kube-proxy` (for `iptables` routing). You must disable **both** so your eBPF TC programs don't conflict with them.

When you write your TC-BPF program in C and use `bpf2go` to inject it into the network interface, it will have total, unobstructed control over packet routing. This environment is exactly what you need to prove latency reduction metrics against standard TCP/IP.

```
Architecture Reminder:
  ┌──────────────────────────┐
  │  DPLS Scheduler (Go)     │  ← Brain: RankU/RankD, priority queue
  └───────────┬──────────────┘
              │ WriteDependencyRuleToKernel()
  ┌───────────▼──────────────┐
  │  Channeler (Go+cilium)   │  ← YOUR TASK 1: pushes rules into kernel
  └───────────┬──────────────┘
              │ bpf() syscall
  ┌───────────▼──────────────┐
  │  eBPF Maps (vault_map,   │  ← Kernel-level vault: routing + retention
  │  retention_map)          │
  └───────────┬──────────────┘
              │ read by TC hook
  ┌───────────▼──────────────┐
  │  TC-BPF Program (C)      │  ← YOUR TASK 2: intercepts UDP packets,
  └──────────────────────────┘     retains fan-out payloads, rewrites dest IPs
```

**Hook choice: TC-BPF** (not XDP, not sk_msg)
- XDP: insanely fast but requires manual raw Ethernet frame parsing — too complex
- sk_msg: only redirects between sockets, does not bypass TCP/IP stack
- **TC-BPF: 95% of XDP latency + has `bpf_csum_diff()` helper (no manual checksum recalculation)**

---

## Prerequisites: VM Specifications

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| OS | Ubuntu 22.04 LTS | Ubuntu 22.04 LTS |
| Kernel | ≥ 5.15 (ships with 22.04) | 5.15.x default ✓ |
| CPU | 2 vCPUs | 4 vCPUs |
| RAM | 4 GB | 8 GB |
| Disk | 30 GB | 50 GB |
| Network | NAT + Host-Only adapter | Same |

> **Critical:** eBPF TC program attachment requires a real Linux kernel. WSL2 can compile Go and C code, but **cannot attach eBPF** to actual network interfaces. All real testing must happen on the Ubuntu VM.

Verify kernel version before starting:
```bash
uname -r
# Must show 5.15.x or higher
```

---

## Phase 1: The eBPF Compiler Toolchain

You need the LLVM compiler to convert your C code into eBPF bytecode, along with kernel headers so your C code knows the layout of network packets.

```bash
# 1. Update your package lists
sudo apt update && sudo apt upgrade -y

# 2. Install the core eBPF compilers and Linux headers
sudo apt install -y clang llvm libbpf-dev gcc-multilib build-essential \
    linux-tools-$(uname -r) linux-headers-$(uname -r) linux-tools-common linux-tools-generic \
    curl git jq unzip
```

**Sanity Check:** Run `bpftool version`. If it prints out a version number, your kernel is ready to interact with eBPF maps.

```bash
bpftool version
# Expected: bpftool v5.15.x, libbpf v0.x.x
```

---

## Phase 2: The Go Environment (The Channeler)

Do **not** install Go using the default Ubuntu `apt` package manager — it is usually too old to support modern `cilium/ebpf` features. Install the latest official release.

```bash
# 1. Download and extract the latest Go version (1.22.2)
wget https://go.dev/dl/go1.22.2.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.2.linux-amd64.tar.gz

# 2. Add Go to your system PATH (run this AND add it to your ~/.bashrc)
export PATH=$PATH:/usr/local/go/bin
export PATH=$PATH:$(go env GOPATH)/bin

# Make it permanent
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc
source ~/.bashrc

# 3. Install the bpf2go code generator — crucial for your Channeler
# bpf2go compiles your C eBPF program and generates type-safe Go bindings
go install github.com/cilium/ebpf/cmd/bpf2go@latest
```

**Sanity Check:** Run `bpf2go -help`. If it displays the help menu, your Go environment is fully ready to bridge with C.

```bash
bpf2go -help
go version
# Expected: go version go1.22.2 linux/amd64
```

---

## Phase 3: The Edge Cluster (K3s)

This is where you must be careful. Standard K3s installs `flannel` (for networking) and `kube-proxy` (for `iptables` routing). You must disable both so your eBPF programs don't conflict with them.

```bash
# Install K3s but explicitly disable the legacy network stack
curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="--flannel-backend=none --disable-network-policy" sh -

# Point your kubectl config to the K3s cluster
mkdir -p ~/.kube
sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
sudo chown $(id -u):$(id -g) ~/.kube/config
```

**Sanity Check:** Run `kubectl get pods -A`. You should see a few pods pending or starting, but the network will currently be broken. **This is exactly what you want before Phase 4** — it means flannel is absent and Cilium hasn't filled the gap yet.

```bash
kubectl get pods -A
# Expected: pods in Pending state (no CNI yet)
```

---

## Phase 4: Cilium (The Network Layer)

Now you install Cilium to take over the network. Because you disabled `kube-proxy` in Phase 3, Cilium will automatically boot in **"strict eBPF mode"**, which provides the cleanest possible environment for your TC hooks.

```bash
# 1. Download the Cilium CLI
CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)
CLI_ARCH=amd64
curl -L --remote-name-all \
    https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CLI_ARCH}.tar.gz
sudo tar xzvfC cilium-linux-${CLI_ARCH}.tar.gz /usr/local/bin

# 2. Install Cilium into your K3s cluster with eBPF replacement enabled
cilium install --set kubeProxyReplacement=true

# 3. Wait for the cluster to stabilize
cilium status --wait
```

**Sanity Check:** Run `cilium status`. It should report `KubeProxyReplacement: True`.

```bash
cilium status
# Expected line: KubeProxyReplacement: True
kubectl get nodes
# Expected: all nodes STATUS = Ready
```

---

## Phase 5: Compile and Verify the TC-BPF Program

With the toolchain ready, compile the actual C program:

```bash
cd ~/dpls-xdp

# Compile tc_bridge.c to BPF ELF bytecode (exact command from execution_log.txt)
clang -target bpf -O2 \
    -I /usr/include/$(uname -m)-linux-gnu \
    -c internal/ebpf/c/tc_bridge.c \
    -o internal/ebpf/c/tc_bridge.o

# Verify output
file internal/ebpf/c/tc_bridge.o
# Expected: ELF 64-bit LSB relocatable, eBPF, version 1 (SYSV), not stripped
```

---

## Phase 6: Run the Go Project

```bash
cd ~/dpls-xdp

# Install Go module dependencies
go mod tidy

# Run full test suite (mock eBPF bridge — works without root)
go test ./... -v

# Run with race detector (required before benchmarking)
go test -race ./...

# Mock end-to-end simulation (no root required)
go run ./cmd/dpls-scheduler

# Real eBPF run (requires root — attaches TC filter to loopback)
sudo ./dpls-scheduler

# Watch BPF trace events in a second terminal while scheduler runs
sudo cat /sys/kernel/debug/tracing/trace_pipe | grep "Intercepted"
```

---

## Three-Node Cluster Setup (For Benchmarking)

Once all three teammates have completed Phases 1–4 on their own VMs, form the cluster:

### Node Roles
| Role | Hostname | Static IP |
|------|----------|-----------|
| Control Plane | `edge-master` | `192.168.56.10` |
| Worker Node 1 | `edge-node-1` | `192.168.56.11` |
| Worker Node 2 | `edge-node-2` | `192.168.56.12` |

### Set Hostnames (each node runs its own line)
```bash
# On edge-master:
sudo hostnamectl set-hostname edge-master

# On edge-node-1:
sudo hostnamectl set-hostname edge-node-1

# On edge-node-2:
sudo hostnamectl set-hostname edge-node-2
```

### Add to /etc/hosts (ALL nodes)
```bash
sudo tee -a /etc/hosts << 'EOF'
192.168.56.10   edge-master
192.168.56.11   edge-node-1
192.168.56.12   edge-node-2
EOF
```

### Get Join Token (edge-master only)
```bash
sudo cat /var/lib/rancher/k3s/server/node-token
# Share this token with edge-node-1 and edge-node-2
```

### Join Workers (edge-node-1 and edge-node-2)
```bash
export MASTER_IP="192.168.56.10"
export NODE_TOKEN="<paste token from edge-master>"

curl -sfL https://get.k3s.io | \
    K3S_URL="https://${MASTER_IP}:6443" \
    K3S_TOKEN="${NODE_TOKEN}" sh -
```

---

## Benchmark Configurations

| Config | What Runs | Proof |
|--------|-----------|-------|
| **C1 – Baseline** | DPLS + standard K3s TCP/IP (Cilium generic, no TC hook) | Measures simulation vs real-hardware gap |
| **C2 – eBPF Bypass** | DPLS + our TC-BPF program on eth0 | Latency improvement from kernel bypass |
| **C3 – eBPF + Retention** | C2 + LRU_HASH fan-out retention map | DAG-driven proactive retention vs CachOf's historical caching |

---

## Quick Troubleshooting

```bash
# TC filter not triggering our BPF program
sudo tc qdisc add dev lo clsact 2>/dev/null || true
sudo tc filter add dev lo ingress bpf obj internal/ebpf/c/tc_bridge.o sec tc direct-action
sudo tc filter show dev lo ingress

# Check K3s + Cilium status
kubectl get nodes
kubectl -n kube-system get pods
cilium status

# eBPF verifier rejection
sudo bpftool prog load internal/ebpf/c/tc_bridge.o /sys/fs/bpf/tc_bridge type tc
# Read verifier output — fix: reduce stack usage, add array bounds checks

# BPF map permission denied
sudo setcap cap_bpf,cap_net_admin+ep ./dpls-scheduler
# Or just run with: sudo ./dpls-scheduler

# View live vault_map contents
sudo bpftool map dump name vault_map

# Reset cluster networking
sudo /usr/local/bin/k3s-uninstall.sh       # master
sudo /usr/local/bin/k3s-agent-uninstall.sh # workers
```

---
