# AgentWorld Runtime Experiments

### From Context Optimization to Behavioral Learning

> Frozen scope: M8 Context Runtime API, Experiment 1, Experiment 2, Experiment 2.1.
> These experiments consume the **real production capabilities** of the runtime.
> **No production code was modified** to make the experiments pass.

---

## 1. Motivation

The core claim of AgentWorld is that an agent does not need the whole world in
its prompt — it needs the right context at the right moment, assembled by a
*Runtime* rather than copied by a prompt builder.

That claim is cheap to state and expensive to prove. These experiments were
built to replace the sentence *"I think this architecture should hold"* with a
chain that is reproducible:

```
Architecture  →  Experiment  →  Data  →  Behavior change
```

The design constraint that matters most for engineering credibility:

- The Context Runtime API is **frozen**.
- Each experiment is an *independent consumer* of the real production paths
  (Compiler, Retriever, Memory store, LLM client, Token estimator).
- Nothing in `worlds/economy/module.go`, the M8 Runtime API, or the production
  planner was altered to make results look better.

This distinguishes the work from RAG demos: the agent's behavior change in
Experiment 2.1 is driven by **Memory written through the real production
`db.AddMemory` path**, not hand-fed fixtures.

---

## 2. Architecture

The Context Runtime compiles context from the world instead of copying it.

```
Perception
   ↓
Intent          (what the agent wants now — drives context)
   ↓
Retrieval       (Intent-driven memory retrieval, filtered by type)
   ↓
Budget          (token budget enforcement per section)
   ↓
Compaction      (optional compression of overflowing sections)
   ↓
Compilation     (assemble the CompiledContext)
   ↓
Provider Adapter (transform Runtime context → provider input)
```

An Agent is a persistent entity, not a single prompt. It carries identity,
personality, memory, goals, relationships, skills, resources, needs — and an
**Intent** that drives what context is assembled for each decision.

Three decision paths are compared in the experiments:

| Arm | Construction | LLM |
|-----|-------------|-----|
| **A** | Full Context (everything loaded) | real |
| **B** | Context Runtime (Intent → Retrieval → Compile → Adapt) | real, same as A |
| **C** | Rule Planner (current Economy heuristic) | none (control) |

**A and B share the same World snapshot, same candidate actions, and the same
LLM client.** Only the Context construction differs, so any decision agreement
is attributable to Context, not to state drift.

---

## 3. Experiment 1 — Context Runtime

**Question:** What does the Context Runtime itself produce, compared to loading
the whole world?

**Setup (controlled, no live LLM):**

- Economy `Perception` as the input snapshot.
- 130 synthetic memories in the controlled `SyntheticMemoryStore`.
- A `WORK` intent (a single, deterministic Think).
- Both Baseline and Context paths share the **same injectable TokenEstimator**,
  so the only variable is whether the Runtime sits between Perception and the
  estimator.

**Result:**

| Metric | Full Context (A) | Context Runtime (B) |
|--------|------------------|---------------------|
| Memories available | 130 | 130 |
| Memories retrieved | 130 (all) | 17 |
| Context tokens | ~2074 | ~318 |
| Reduction | — | **~4.4× smaller context** |
| Stable prefix | — | 1 hashable prefix (KV-cache ready) |

Noise leakage: **0** (retrieved memories were all type-relevant to the intent).

> **Condition note:** The 4.4× figure is specific to this synthetic benchmark
> (130 memories → 17 retrieved → 318 tokens). It is a *controlled, reproducible*
> measurement, not a universal guarantee about all worlds.

---

## 4. Experiment 2 — Decision Preservation

**Question:** Does the Context Runtime preserve decision quality while cutting
context cost, when a **real LLM** is in the loop?

**Setup:**

- Real LLM (`deepseek-chat` via the OpenAI-compatible endpoint, read from the
  same `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` env vars the production
  runtime uses).
- A = Full Context + LLM; B = Context Runtime + LLM; C = Rule Planner (control).
- B reuses **A's Intent** (`intentFromDecision`). This removes the possibility
  that a divergence is caused by B guessing a different intent than A.

**Issue found and fixed during testing:** the production `llm.Client.Decide`
hardcodes `response_format: json_object`, which the model backend routes
`deepseek-chat` to (`deepseek-v4-flash`) rejects with HTTP 400. The experiment
added an *independent, non-JSON* `decideText` path that reuses the same
endpoint/key/model. **The production `Decide` was not changed.**

**Result (verified across repeated runs):**

| Metric | Value |
|--------|-------|
| Full vs Runtime context | ~2× reduction |
| A / B Decision Agreement | **100%** |
| C (Rule) runs as control | yes |

Interpretation: with ~2× less context, the Runtime agent makes the *same
decision* as the full-context agent, 100% of the time on the tested scenarios.

> Boundary respected: B's intent is taken from A's decision, so agreement is a
> measure of Context faithfulness, not of the LLM agreeing with itself.

---

## 5. Experiment 2.1 — Memory-Driven Behavior

**Question:** Can an agent's own experience change its behavior — i.e. does the
Agent Loop actually close?

```
World produces Experience  →  Memory  →  Retrieval  →  Context  →  Decision
```

### 5.1 — 2.1a Retriever behavior (deterministic)

Validates that Intent-driven retrieval returns **only** relevant memories and
**zero** noise, even under load.

- Dataset: 5 relevant + 100 unrelated noise memories (per `DefaultSyntheticConfig`).
- Same retriever implementation as the runtime, just a different backing store.
- Tested for both `WORK` and `HIRE_AGENT` intents.

| Intent | Retrieved | Relevant | Unrelated (noise) |
|--------|-----------|----------|-------------------|
| WORK | 20 | 20 | **0** |
| HIRE_AGENT | 20 | 20 | **0** |

**Verdict:** `Retrieved>0` and `Unrelated=0` for both intents → **PASS**.

### 5.2 — 2.1b Cold vs Warm Agent Loop (real Memory)

This is the genuine loop, not a RAG demo:

1. **Cold** — empty Memory, same intent the World would form. Sampled `N` times
   for a majority decision (defeats single-run LLM temperature variance).
2. **World produces Experience** — 3 consecutive loss-making HIRE contracts
   written through the **real production `db.AddMemory`** path
   (`RecordExperience`), never hand-fed.
3. **Warm** — same perception/intent, Memory now holds the losses. Sampled `N`
   times for a majority decision.

To make the scenario deterministic and the flip clean, the Economy `money`
parameter was set to a level where the cold agent reliably chooses to HIRE.

**Result (verified, e.g. at money=2000):**

| Phase | Memory | Retrieved | Majority decision |
|-------|--------|-----------|-------------------|
| Cold | 0 memories | 0 | **HIRE_AGENT** |
| Warm | 3 real loss experiences | 3 relevant / 0 noise | **DO_JOB** |

**Verdict:** Memory retrieved in Warm (`>0`), and the majority decision flipped
`HIRE_AGENT → DO_JOB`. **The loop closed: the agent changed behavior based on
its own experience.**

---

## 6. Results

| Experiment | Result |
|------------|--------|
| Context Runtime | ~4.4× smaller context |
| Retrieval | 17 / 130 memories |
| Noise leakage | 0 |
| Stable Prefix | 1 |
| Full vs Runtime | ~2× context reduction |
| Decision Agreement | 100% |
| Cold → Warm | HIRE → DO_JOB |
| Memory retrieval | 3 relevant / 0 noise |
| **Production path modified** | **No** |

The last row is the most important one. These experiments were **not** built by
rewriting production code into experiment code to make a point. The Runtime API
is frozen; the experiments independently consume real production capabilities.
In a software engineering project, that distinction is worth more than any
headline number.

---

## 7. Limitations

These experiments are **controlled benchmarks, not universal claims about LLM
behavior.** We state the boundaries explicitly because they increase confidence
rather than reduce it:

- **Current Economy scenario is limited.** All behavioral results are observed
  in one world (Economy) with a narrow set of intents (`WORK`, `HIRE_AGENT`).
- **Sample size is still limited.** Repeated runs use majority voting to defeat
  LLM temperature variance, but the absolute number of scenarios is small.
- **Decision Agreement is not a complete measure of long-term behavior quality.**
  100% agreement means the Runtime preserves the full-context decision; it does
  not prove the decision is *good* over many turns.
- **Token counts are partly estimated.** Some figures use a rough token
  estimator (`RoughEstimator` / `EstimatorFromToken`), not exact provider
  accounting.
- **Not yet cross-model validated.** Results use `deepseek-chat`; behavior may
  differ on other providers.
- **Not yet validated on complex multi-turn worlds.** The loop was tested over a
  single decision point, not a long horizon.
- **Not yet validated at larger Memory scale.** Retrieval was stressed to 100+
  noise memories; much larger stores may change retrieval quality.

---

## 8. Reproducibility

All experiments are self-contained Go programs under `experiments/`.

**Environment (same as the production runtime — no key is printed or persisted):**

```bash
export LLM_API_KEY="..."      # required for A/B arms (Experiments 2 and 2.1)
export LLM_BASE_URL="https://api.deepseek.com/v1"   # default if unset
export LLM_MODEL="deepseek-chat"                     # default if unset
```

**Experiment 1 (no LLM required):**

```bash
go run ./experiments/m8/cmd/m8
```

**Experiment 2 (requires LLM):**

```bash
go run ./experiments/m8exp2/cmd/m8exp2 -agent alice -repeat 3 -money 320
```

**Experiment 2.1 (requires LLM; `-exp21` selects the memory loop):**

```bash
go run ./experiments/m8exp2/cmd/m8exp2 -exp21 -agent alice -repeat 3 -money 2000
```

**Conditions attached to each headline number:**

| Number | Condition |
|--------|-----------|
| ~4.4× / 318 tokens / 17 of 130 | synthetic 130-memory Economy benchmark, `WORK` intent, single Think |
| ~2× reduction | real LLM A/B on the Economy snapshot |
| 100% agreement | scenarios where B reuses A's intent; repeated runs |
| COLD→WARM flip | Economy `money=2000`, 3 HIRE-loss experiences via production `db.AddMemory` |
| 0 noise | 2.1a dataset = 5 relevant + 100 unrelated; both `WORK` and `HIRE_AGENT` intents |

No external services beyond the LLM endpoint are required; Memory uses an
in-memory SQLite store, so runs are hermetic and repeatable.

---

## 9. What's Next

Feature development is **frozen** at this point. The architecture → experiment →
data → behavior-change chain is established and documented. The next phase is
reserved for:

- **Pascal World** — a high-difficulty validation ground for the Agent Loop:

  ```
  Issue → Intent → Retrieve code/experience → Context → Write Pascal
        → Compile → Test → Failure → Memory → Retry → Improved behavior
  ```

  The interesting question is not "can an AI write Pascal" but whether an agent,
  through repeated compile failures stored in its own Memory, gradually forms a
  durable programming strategy. That is the same loop proven in Experiment 2.1,
  observed at higher difficulty.

Until then: no M8.11, no M9, no additional Retriever work, no multi-provider
work, no new Compaction, no new World. The runtime stays frozen; the experiments
stay as the evidence.

The design for the next phase lives in
[`docs/pascal-world-design.md`](./pascal-world-design.md) — it is the blueprint
for *after* the freeze and depends directly on the Memory-Driven Behavior loop
proven in Experiment 2.1.

---

## Appendix A — Pascal World v0.1 Smoke Test (post-freeze)

Pascal World v0.1 was implemented as the first consumer of the **same frozen
production Runtime paths** used by the experiments above — `Memory` (`db.AddMemory` /
`Retriever`), `Issue`/`Perception`, `Context`, and the `LLM` client. **No
production code was modified.** What is new is a *World module* (`worlds/pascal`)
that wires the Agent Loop to a **real Free Pascal Compiler (FPC)** running inside
WSL as the "physical law" of the world.

The product being validated is the **Runtime**, not an "LLM → Pascal generator":
FPC is only the environment's physics; the agent must perceive the issue, retrieve
its own past experience, assemble context, write real Pascal, compile (real FPC),
test (real FPC), and on failure change strategy on the next loop.

### A.1 Setup

- Agent: 1 (`PascalDev`), Issues: 5 (`#001`–`#005`).
- `#005 Broken.pas` is a deliberately-compiling-broken unit; `#001` carries a
  subtle off-by-one so it is easy to "fix" wrongly on the first attempt.
- FPC 3.2.2 executed via `wsl -d Ubuntu-22.04` — every compile/test is real.
- Budget: `investigateBudget = 3`, `afterFailBudget = 2` (per issue).

### A.2 Results (1 Agent × 5 Issues)

| Issue | thinks | compiles | compile_failures | test_failures | retrieved_memory | final_success |
|-------|-------:|---------:|-----------------:|--------------:|-----------------:|:-------------:|
| #001  | 4      | 1        | 0                | 0             | 0                | true          |
| #002  | 3      | 1        | 0                | 0             | 1                | true          |
| #003  | 2      | 1        | 0                | 0             | 2                | true          |
| #004  | 3      | 1        | 0                | 0             | 3                | true          |
| #005  | 5      | 2        | 1                | 0             | 5                | true          |

### A.3 Observations

- **All 5 issues resolved** through the real compile/test loop; FPC is the ground
  truth, no simulation.
- **Memory loop is live across issues**: `retrieved_memory` climbs `0 → 1 → 2 → 3 → 5`
  as the agent accumulates and retrieves its own failure/repair experience — the
  exact mechanism proven in Experiment 2.1, now observed at higher difficulty.
- **Failure → strategy change is real**: `#005` records 1 genuine `compile_failures`
  (Broken.pas), the agent perceived the FPC error, and on the next loop applied a
  corrected write (thinks 5, final success). The loop is not a straight line.
- **Tool usage is genuine**: every fix went through `write_file` → `compile` →
  `test`; the agent never bypassed the real compiler.

### A.4 Scope discipline

- **Production path modified: No.** The Runtime API, `Memory`, `Retriever`,
  `Context`, and the production planner are untouched. `worlds/pascal` is an
  additive World module.
- This appendix is an *extension* of the frozen evidence, not a mutation of it.

---

### A.5 Experiment 1 — Cold vs Warm

The real question for Pascal World: does accumulated experience actually improve
the agent's software-engineering behavior, or is the Memory loop merely
cosmetic? We ran both conditions on the **same 5 issues, same agent, same FPC**:

- **Cold** — Memory cleared before every Issue (`ClearMemory`), so each Issue is
  solved with zero prior experience.
- **Warm** — Memory retained across Issues (the Cold run's experiences stay),
  then the same 5 Issues are solved again, now with retrievable history.

#### A.5.1 Raw data (per-Issue `retrieved_memory`)

| Issue | Cold retrieved | Warm retrieved |
|-------|:--------------:|:--------------:|
| #001  | 0 | 2 |
| #002  | 0 | 3 |
| #003  | 0 | 4 |
| #004  | 0 | 5 |
| #005  | 1 | 7 |
| **sum** | **1** | **21** |

#### A.5.2 Aggregate comparison

| Metric | Cold | Warm | Δ |
|--------|-----:|-----:|---|
| Think (sum) | 16 | 14 | ↓ 12.5% |
| Compile failures | 1 | 1 | = |
| Test failures | 0 | 0 | = |
| Retrieved memories | 1 | 21 | ↑ 21× |
| Context tokens (sum) | 8984 | 10196 | ↑ 13.5% |
| Success | 5/5 | 5/5 | = |

#### A.5.3 Honest reading

- The Memory loop is **real and dense** in Warm: the agent retrieved **21**
  experiences vs **1** in Cold — the Retriever and `db.AddMemory` path work
  end-to-end under a real compiler.
- But the **behavioral uplift is small**: Think dropped only ~12.5%, compile/test
  failures were identical, and success was already 5/5 in both. Warm's context is
  actually *larger* (more retrieved memories), not smaller.
- Interpretation: for these 5 localized Pascal issues, past repairs are weakly
  transferable — the agent mostly re-derives each fix from the code + compiler
  error rather than from memory. This is the **correct, unvarnished result**:
  Memory changes *what the agent sees* (21 vs 1 retrieved), but does not yet
  measurably change *what it does next* for this task class.

This is exactly why the experiment matters more than a demo: it produces a
**reproducible null-ish result**, not a slogan. The next step is to make the
stored experience *actionable* (e.g. cache known-good fix patterns, surface the
nearest past compile error), so that Warm's retrieved memories translate into
fewer thinks and fewer failures — which would complete the chain:

```
M8 Context smaller
   ↓
Exp 2 decision preserved
   ↓
Exp 2.1 Memory changes behavior
   ↓
Pascal World Memory changes SWE behavior   (this appendix: loop proven, uplift pending)
   ↓
Cold/Warm  (this section: experience retrieved 21×, uplift to be earned)
```

Until that uplift is earned, Pascal World v0.1 stays **frozen** at Smoke Test +
Cold/Warm baseline. No M9.

---

## Next Research Question — *Experience → Behavior* (not M9)

The cold/warm null-ish result is not a dead end; it is the **research question
made precise**. The current loop is:

```
Experience → Memory → Retriever → Context → LLM → Decision
```

What we have actually proven so far is only the first half of the value chain:

```
Experience → Memory → Context ↑        (proven)
Experience → Behavior ↑                (NOT yet proven)
```

Pascal World is the natural laboratory for the second arrow, because it has a
**real, falsifiable outcome** (does the code compile and pass under real FPC?).

### From Knowledge to Operational Experience

Today a memory entry is effectively: *"I have seen this error before."* The agent
is not told *"so what should I do differently this time?"* To close the gap, we
propose **structuring experience** into an operational record:

```
Problem     : unit name mismatch
Action      : write_file(DateUtils.pas)
Failure     : FPC Error: Illegal unit name
Cause       : unit declaration != filename
Resolution  : normalize unit header
```

On the next issue, the Retriever surfaces this record and the agent can form the
intent `DEBUG`, prioritize checking the unit declaration, and skip the mistake
it already paid for. Memory then shifts from **Knowledge** to
**Operational Experience** — experience that prescribes an action, not just a fact.

### The next thesis: *From remembering to learning.*

What is already standing:

```
Experience → Memory → Retrieval → Context        ✅ proven
```

What the next phase must prove:

```
Experience → Operational Memory → Retrieval → Context → Changed Decision → Changed Outcome
```

The jump from "memory grows context" to "memory changes the decision and its
outcome" is the whole point. That is the difference between **remembering** and
**learning**.

### Experiment design — FROZEN (single-variable)

We already have real results for A and B from the Cold/Warm run. The next step
changes **exactly one variable**: *how experience is represented.* Everything
else is held constant so that any difference can be attributed to representation,
not to a moved goalpost.

| Held constant | Value |
|---------------|-------|
| Agent | the same `PascalDev` agent |
| Issues | the same 5 issues (#001–#005) |
| Compiler | the same real FPC 3.2.2 via WSL |
| LLM | the same model / endpoint |
| Context budget | the same `investigateBudget` / `afterFailBudget` |
| Retriever | the same retrieval config |
| Tool set | the same `read_file / search_code / compile / test / write_file` |

| Group | Memory contents | Status |
|-------|-----------------|--------|
| **A — No Experience** | 0 memories | ✅ already measured (Cold) |
| **B — Raw Memory** | original history records | ✅ already measured (Warm) |
| **C — Operational Memory** | `Problem + Action + Failure + Cause + Resolution` | ✅ implemented (single-variable only) |

Group C is the crux. It does not merely tell the agent *"I have seen this
before"*; it tells it *"here is what I hit, what I did, why it failed, the root
cause, and how it was finally resolved"* — an experience that prescribes an
action, not just a fact.

**Single-variable implementation.** The Retriever, Compiler, LLM, Agent decision
logic and Issue set are *untouched*. The only change is what `remember()` writes:
group B writes free text (`"Compile failed: …"` / `"Resolved …"`); group C renders
the same event through `OperationalMemory.Format()` into a
`PROBLEM / ACTION / FAILURE / CAUSE / RESOLUTION` narrative. The Retriever still
matches by text similarity — so any uplift in C can be attributed to
*representation*, not to a moved goalpost.

Compare across groups:

```
Think | Compile Fail | Test Fail | Token | Success | Recovery
```

We froze the design first (A and B already had real numbers), then changed the
single variable. This is what lets us say, with some confidence:

> The difference came from how experience was represented —
> not from a different prompt, model, task, or Runtime.

### Three possible outcomes (all publishable)

We pre-commit to reporting whichever result appears:

- **Outcome 1 — C clearly improves behavior.** Raw memory → little change;
  operational memory → Think ↓ / Compile Fail ↓ / Recovery ↑. *The ideal result:
  structured experience is what turns memory into learning.*
- **Outcome 2 — C still shows no improvement.** Also valuable: it would mean
  *structured experience ≠ learning*, and the real bottleneck is elsewhere —
  Decision, Planning, or Belief Update. The loop would point us there next.
- **Outcome 3 — C reduces Think / Token but not success rate.** Shows experience
  first improves **efficiency**, not **capability**. A clean, useful distinction
  in its own right.

The project's stance stays the same: *ugly results get published too.* To date,
the most valuable thing AgentWorld produces is not "what it can do," but that it
can answer, with repeatable experiments, **why an agent's behavior actually
changes.**

### Phase status

```
AgentWorld
│
├── Runtime                 ✅
├── Context Runtime         ✅
├── Economy World           ✅
├── Social World            ✅
├── Pascal World            ✅
│
├── Memory → Context        ✅
│
└── Experience → Behavior   ← current research question (no M9)
```

### The line of research, stated

```
M8  Context Runtime              ✅
  ↓
Exp 2  Decision preserved        ✅
  ↓
Exp 2.1  Memory → Behavior       ✅
  ↓
Pascal World v0.1                ✅
  ↓
Cold / Warm                      ✅  (null-ish result: retrieved 21×, behavior ~flat)
  ↓
Experience → Behavior            ←  NEXT (From remembering to learning)
       A ✅  B ✅  C ✅  (single-variable, design frozen, implemented)
```

The cold/warm null result is **preserved as-is**; it is the honest baseline that
makes the next question meaningful. The guiding question for this phase:

> **When does experience actually change what an agent does?**

---

### A.6 Experiment 2 — A/B/C implemented (Experience → Behavior)

The single-variable experiment is now implemented in `worlds/pascal`:

- **`opsmem.go`** — the *only* new representation layer. `OperationalMemory`
  carries `Problem / Action / Failure / Cause / Resolution`; `Format()` renders it
  as a narrative. No Runtime, Retriever, Compiler, LLM, or Agent code was touched.
- **`Agent.SetMemMode`** — flips `memRaw` (B) / `memOperational` (C). `remember`
  then writes either free text (B) or the operational narrative (C); `memNone` (A)
  is achieved by clearing Memory before every Issue.
- **`World.ABCExperiment(group)`** / `cmd --abc A|B|C` — runs all Issues under one
  group, holding everything else constant.

#### A.6.1 Richer metrics (beyond the original six)

Each `SmokeRecord` now also carries:

| Metric | Meaning |
|--------|---------|
| `recovery_attempts` | fixes attempted after a failure (afterFail writes) |
| `repeated_failure` | same compiler error repeated (no learning between tries) |
| `first_action_correct` | first write reached success without any afterFail repair |
| `memory_mode` | raw / operational / none |
| `replay` | per-think chain: retrieved → context_tokens → decision → action → result |

#### A.6.2 Replay — the core diagnostic

Every Think appends a `ReplayFrame`:

```
Issue → Retrieved Experience → Context → Decision → Action → Compiler/Test Result
```

This is what makes the experiment *interpretable*, not just scored. When C and B
diverge, we can replay: *what did B see? what did C see? why did C decide
differently?* — the decisive metric is whether a retrieved experience changed the
agent's next action.

#### A.6.3 Issue set grown to 10

From 5 to 10 real Pascal issues (all on `projects/hotelutils`, each with a unit +
a unit test compiled with `-Sa` so `Assert` is actually enforced). Bugs span
off-by-one, integer-division, boundary, string-indexing, and divide-by-zero.
Target: 20–50 issues for stronger statistics; the current 10 removes the
small-sample risk of the original 5 while staying verifiable.

Run:

```bash
cd worlds/pascal/cmd/pascal
PASCAL_USE_WSL=1 go run . --abc A     # No Memory
PASCAL_USE_WSL=1 go run . --abc B     # Raw Memory
PASCAL_USE_WSL=1 go run . --abc C     # Operational Memory
```

#### A.6.4 Status — design & code ready, numbers pending

The framework, the single-variable guarantee, the richer metrics and the replay
log are in place and compile. The **three-group run has not yet been executed to
completion** (it requires a full LLM + FPC pass over 10 issues × 3 groups). The
result — whichever of the three outcomes in §"Three possible outcomes" appears —
will be filled in here once run, and published regardless of direction.

> Design principle preserved: *ugly results get published too.* The A/B/C
> comparison is valuable precisely because it can disprove the hypothesis that
> "structured memory → better behavior."
