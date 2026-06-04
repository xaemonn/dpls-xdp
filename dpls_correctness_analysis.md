# DPLS Algorithm Correctness Analysis
## Comparing `internal/scheduler/core.go` vs. Liu et al. (IEEE IoTJ 2026)

> **Verdict: Partially correct — 5 deviations found, 3 are bugs.**

---

## The Paper's Algorithm (Summary)

### Algorithm 1 — Priority Initial Value

**RankU (Upward, Eq. 13):** Uses **WORST-CASE** times (fmin, λmin)
```
RankU(vsink) = τ̂(vsink)                       ← worst exec time of sink
RankU(v)     = τ̂(v) + max over succ u { τ̂_comm(v→u) + RankU(u) }
```

**RankD (Downward, Eq. 15):** Uses **WORST-CASE** times
```
RankD(vsrc)  = 0
RankD(v)     = max over pred u { τ̂(u) + τ̂_comm(u→v) + RankD(u) }
```

**Subtask Priority (Eq. 19):**
```
p(v) = Rank(v) + I(v) + vol(Gi)/|Vi|

where:
  Rank(v) = RankU(v) + RankD(v)    ← static
  I(v)    = t_start(v) - max{ t_finish(u) | u ∈ pred(v) }  ← dynamic contention level
  vol(Gi)/|Vi| = (sum of all τ̂) / (number of subtasks)    ← task volume per subtask
```

**Worker Assignment:**
- **Critical subtask:** assign to worker with minimum Finish Time (EFT)
- **Non-critical subtask:** assign to worker that becomes available **earliest** (not EFT)

---

## Bug Analysis — Five Deviations

### BUG 1 (Critical): RankU uses average times, paper requires worst-case times
**Paper says:** `τ̂(v) = r_v / f_min` and `τ̂_comm = d / λ_min`  
**Code does:** `wBar = BaseComputation / avgComp` — divides by AVERAGE, not minimum

```go
// WRONG (line 329)
wBar := float64(node.BaseComputation) / avgComp  // ← average compute multiplier

// CORRECT — paper uses worst-case (slowest worker = lowest f)
wBar := float64(node.BaseComputation) / minComp  // ← minimum compute multiplier
```

```go
// WRONG (line 333)
cBar := float64(succ.DataSize) / (avgBand * 1000)  // ← average bandwidth

// CORRECT — paper uses worst-case (slowest link = minimum bandwidth)
cBar := float64(succ.DataSize) / (minBand * 1000)  // ← minimum bandwidth
```

**Impact:** Rank values are artificially deflated. The critical path is underestimated, so non-critical tasks may be misclassified as critical.

---

### BUG 2 (Critical): Priority formula is wrong — missing vol(Gi)/|Vi| term
**Paper says (Eq. 19):**
```
p(v) = Rank(v) + I(v) + vol(Gi)/|Vi|
```
**Code does:**
```go
// HIGH CONTENTION (line 413):
task.DynamicPriority = task.StaticRankU + aging
// Missing: + StaticRankD, + I(v), + vol/|V|

// LOW CONTENTION (line 416):
task.DynamicPriority = (task.StaticRankU + task.StaticRankD) + aging
// Missing: + I(v), + vol/|V|
```

The paper's `p(v)` is a **single unified formula** with three components:
1. `Rank(v) = RankU + RankD` — always included
2. `I(v)` — the contention level (real-time waiting time) — **never computed**
3. `vol(Gi)/|Vi|` — task volume per subtask — **never computed**

The code's high-contention/low-contention split is an approximation invented during development, not in the paper.

**Impact:** Priority ordering is wrong. The `I(v)` term is specifically designed to prevent starvation of tasks with long-waiting predecessors.

---

### BUG 3 (Critical): Contention level I(v) is never computed
**Paper (Def. 7, Eq. 18):**
```
I(v) = t_start(v) - max{ t_finish(u) | u ∈ pred(v) }
```
This is the **actual waiting time** of the subtask — how long it sat in the queue after all its predecessors finished.

**Code:** The `aging` variable approximates this with `waitTime * AgingFactor` (time since ReadyAt). But:
- The paper's `I(v)` is relative to predecessor finish times, not from ReadyAt
- The aging factor `α` is a custom addition, not in the paper at all

**Impact:** Dynamic re-prioritization doesn't match the paper's mechanism.

---

### BUG 4 (Moderate): Non-critical subtask assignment strategy is wrong
**Paper says:**
- Critical subtasks → assign to worker with **minimum finish time (EFT)**
- Non-critical subtasks → assign to worker that **becomes available earliest** (EST-based)

**Code:** `selectOptimalWorkerForTask()` always uses EFT for all tasks (same formula for both critical and non-critical). There is no distinction between critical and non-critical path tasks.

---

### BUG 5 (Minor): `computeDownwardRank()` misses the entry node's own successors
**Paper (Alg. 1, Steps 9-15):** The downward rank loop goes `from vsrc to vsink` in topological order.
**Code (lines 382-388):** Only starts `computeDownwardRank` on the SUCCESSORS of entry nodes, skipping setting `StaticRankD = 0` on the entry nodes themselves before recursing.

This is partially handled but the recursion can re-visit nodes and overwrite already-computed values.

---

## What Is Correct ✅

| Feature | Status |
|---------|--------|
| RankU recursive formula structure | ✅ Correct direction (sink → src) |
| RankD recursive formula structure | ✅ Correct direction (src → sink) |
| `Rank(v) = RankU + RankD` | ✅ Correct (used in low-contention path) |
| Heap-based max-priority queue | ✅ Correct |
| Dependency tracking (DecrementIndegree) | ✅ Correct |
| EFT-based worker selection logic | ✅ Correct for critical tasks |
| Communication cost formula | ✅ Correct structure `d/bandwidth` |
| Entry tasks (indegree=0) queued on DAG arrival | ✅ Correct |

---

## Summary Table

| Paper Component | In Code? | Correct? | Severity |
|----------------|----------|----------|----------|
| RankU worst-case timing (f_min, λ_min) | ❌ Uses avg | Wrong | **Critical** |
| RankD worst-case timing | ❌ Uses avg | Wrong | **Critical** |
| Priority = Rank + I(v) + vol/\|V\| | ❌ Custom formula | Wrong | **Critical** |
| Contention level I(v) | ❌ Missing | Missing | **Critical** |
| vol(Gi)/\|Vi\| task volume term | ❌ Missing | Missing | **Critical** |
| Critical vs non-critical scheduling | ❌ Not distinguished | Wrong | Moderate |
| Criticality set Criti(Gi) | ❌ Not computed | Missing | Moderate |
| Downward rank base case | ⚠️ Partial | Partial | Minor |
