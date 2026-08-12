# Economy World — virtual economy

AgentWorld's second world, probing a more important question: **when agents face resource constraints, do they autonomously change their behavior to benefit themselves?**

[English](README_EN.md) · [中文](README.md)

20 agents (Engineer / Farmer / Trader / Courier / Doctor / Miner / Chef) produce, trade, earn and spend in one shared economy, and use the **Skill System** to autonomously pick the work they are actually able to do.

**M5 Skill Economy**: skills have a **market price** — agents autonomously decide whether to spend their own coins to **invest in a new skill**.

**M6 Agent Labor Market**: agents go one step further — **learn to use other agents' abilities** (hiring + contracts + escrow), making *Buy Skill vs Hire Agent* a real economic decision fork.

## The core question

> Put "money" into AgentWorld — will agent behavior emerge as a change?
> (M5) Will an agent invest its hard-earned coins in a **new skill**?
> (M6) Will an agent **hire someone else** instead of buying the skill itself — and will 100 agents make **different choices** facing the same opportunity?

Phase-1 goal: **20 agents + starting wealth + 10 jobs/goods + autonomous trading + Observatory + Skill Economy + Agent Labor Market**.

![Economy Observatory overview: wealth rank + total assets + transactions](screenshots/01-overview-wealth-rank.png)

## Two worlds in contrast

| | Driven by | Decision basis |
|---|---|---|
| **GooseGame** | survival & victory | hidden identity + info isolation + social deduction |
| **Economy World** | resources & wealth | balance + prices + skills + job opportunities |

Both run on the same AgentWorld Runtime — both have Why + DecisionRecord + Observatory.

## M5 Skill Economy MVP — the skill market

Turn skills into **investable assets**: agents start with **only their own profession skill**; to learn any other skill they must spend their own coins at the **Skill Marketplace**.

### Key change: `defaultSkills` grants only the profession skill

> This is the most important cut of M5. In M7 an agent started with all skills (own Lv7 + rest Lv2), which left **no reason to invest in new capabilities**. M5 grants only the own-profession skill at Lv3, so earning more means deciding "should I buy a skill, and which one?"

```
Courier Agent
├── courier Lv3          # the only starting skill
├── engineer ❌           # wants to repair → must spend 100 coins at the market
├── doctor   ❌
└── miner    ❌
```

### Fixed skill prices (no volatility in v1)

Prices follow "earning potential" (higher earners cost more), so we measure **whether agents do skill investment**, not whether they adapt to price swings:

| Skill | Price(coins) | Reference job reward |
|---|---|---|
| Courier | 40 | Collect Data 15 / Deliver Package 10 |
| Farmer | 50 | Harvest Crops 20 |
| Trader | 60 | arbitrage (no fixed job) |
| Chef | 60 | Cook Meal 14 |
| Miner | 80 | Mine Ore 35 |
| Engineer | 100 | Repair Reactor 40 |
| Doctor | 120 | Medical Treatment 50 |

### Decision pipeline: market perception → economic evaluation → investment decision

The Planner does **not hardcode "buy Engineer"**. It gets a **structured result** from a unified `evaluate_skill`, then decides:

```
Skill Marketplace (fixed prices)
        │
        ▼
 Market Perception (current capabilities + market opportunities)
        │
┌───────┴────────┐
capabilities     opportunities
└───────┬────────┘
        ▼
    evaluate_skill       ← unified evaluation (structured output)
        │
   ┌────┴────┐
 NOT_BUY      BUY
   │         │
 keep work   buy_skill
             │
             ▼
        new skill (Lv1)
             │
             ▼
        new jobs appear
             │
             ▼
       new income (levels up)
             │
             ▼
        next decision
```

`evaluate_skill` returns a structure (like `evaluate_job` / `evaluate_trade` — makes the runtime a decision system, not a pile of ifs):

```
Skill: Engineer
Price: 100
Current Balance: 135
Current Income: 13/job
Expected Additional Income: +27/job
Payback: ~3 jobs
Investment Risk: Medium
Recommendation: BUY / NOT_BUY
```

Evaluation dimensions: **can afford** (balance ≥ price), **is it worth it** (new-skill earning potential vs current income lift),
**risk** (how much is left after buying, bankruptcy risk), **payback** (jobs to break even), **personality** (adventurous vs cautious).

### Skill proficiency evolution

A purchased skill starts at **Lv1** and levels up by completing that class of job (practice → proficiency → higher success/income).
So "invest → needs time to break even → returns appear gradually". Agents that buy a skill with no matching jobs, or buy the wrong skill, lose money.

### Experiment acceptance (what the Observatory answers)

After one full run, the Observatory answers:

- **Who bought skills?** (Skill Marketplace panel)
- **Who didn't, and why?** (Inspector "skill market" perception)
- **How much did buyers earn?** (investment return panel: invested / skill-earned / net return)
- **Who bought wrong / who invested well?** (negative net return = wrong; net ≥ invested = success)
- **Which skill is most valuable / scarcest?** (purchase-feed statistics)

The ideal outcome is **not** everyone learning the "right answer". It's different agents, under **limited info, limited funds, different personalities**, producing **different economic strategies** (some buy Engineer, some buy Doctor, some stay put, some buy wrong and go bankrupt) — that is the real thing worth studying.

![Skill marketplace + purchase feed + investment return](screenshots/03-skill-marketplace.png)

## M5.1 Skill Economy Core — level gates + income multiplier + scarcity

M5 built the skill market, but the **level dimension** of a skill was not yet a real economic resource. M5.1 adds three pieces so "skill level" truly drives what jobs you can do / your success / your income:

### 1. Job skill-level gates

The same skill has jobs across level tiers — **higher level unlocks higher-paying jobs** (a prerequisite for the M6 hiring mechanism):

| Skill | Lv1 jobs | Lv3 jobs | Lv5 jobs |
|---|---|---|---|
| Engineer | Repair Machine 35 | Repair Reactor 60 | Engineering Project 100 |
| Doctor | First Aid 30 | Medical Treatment 55 | Surgery 90 |
| Courier | Deliver 10 / Collect 15 | — | Fleet Transport 30 |

`DoJob` enforces the gate: **skill level < job MinLevel → cannot do it** (the "level dimension" of skill isolation).

### 2. Skill Level → income multiplier

Reward = base reward × `IncomeMultiplier(level)` (Lv1:1.0 → Lv3:1.5 → Lv5:2.2 → Lv7:3.0).
**Leveling up a skill is itself an investment** — the same job pays more at higher levels.

```
Engineer income: Lv1=35  Lv3=90  Lv5=220   ← 6x gap (verified by regression test)
```

### 3. Skill scarcity statistics

`SkillOffer` gains `Owners / Demand / Scarcity`:
- **Owners**: how many agents have the skill (fewer = scarcer)
- **Demand**: total open-job rewards for the skill (demand strength)
- **Scarcity** = Demand / Owners

Injected into perception + public market; `evaluateSkill` awards a bonus for scarce skills.

### M5.1 experiment

```
Skill scarcity (100 agents, a snapshot):
  Doctor   owners=4  demand=265  scarcity=66.25  ← very scarce
  Engineer owners=4  demand=160  scarcity=40     ← scarce
  Farmer   owners=11 demand=0    scarcity=0       ← oversupplied (no demand)
```

Regression test `m51_test.go` (4 cases): multiplier table / Engineer 3 tiers / level gate / income growth.

## M6.1 Agent Labor Market — teaching agents to use others' abilities

M5 asks "should I invest in myself"; M6 asks "**should I use someone else's ability**".

### Data model

- **Service**: a hireable service (skill / min level / **fixed price**, cheaper than buying the skill — otherwise nobody would hire)
- **AgentService**: having the skill automatically enables offering the service (no extra registration)
- **Contract**: a hire agreement (employer / worker / service / price / status)
- **Escrow**: the employer's service fee is locked into the contract — **paid to the worker on success / refunded on failure** (money is conserved)

```
Bob needs Repair Machine (no engineer)
        │
        ▼
   Labor Market (Alice Lv5 ¥20 / Charlie Lv3 ¥15 / David Lv1 ¥8)
        │
        ▼
  Planner compares: Buy Engineer=100  vs  Hire Alice=20  vs  Wait=0
        │
        ▼
      hire_agent(Alice) → Contract → Escrow 20 → Alice executes → Bob -20 / Alice +20
```

### hire_agent perception & execution

- `Perception` injects `Services` (labor market) + `WorkersBySkill` (hireable workers)
- `HireAgent`: checks balance / worker skill / level → creates a Contract + Escrow deduction
- `ExecuteContract`: worker executes by skill success rate; success pays worker / failure refunds employer

## M6.2 Unified Economic Decision — Buy / Hire / Wait evaluated together

Core change: **removed the `balance >= 1.2×price` hand-crafted threshold**. The Planner scores every candidate uniformly and decides on its own — we don't write the strategy for the agent.

```
              Economic Decision
                    │
      ┌─────────────┼─────────────┐
      ↓             ↓             ↓
  Buy Skill     Hire Agent       Wait
      │             │             │
      └─────────────┼─────────────┘
                    ↓
          UnifiedScore (cost/return/risk/future/profession/personality)
                    ↓
                 Planner selects
                    ↓
                 Action
```

Each candidate is an `EconomicOption` (Cost / Reward / Future / Risk / Score). Key scoring dimensions:
- **Affordable** + payback + income uplift
- **Risk** (how much is left after buying / hiring)
- **Profession synergy** (+0.2 for own-profession skill, +0.08 for adjacent) — different professions prefer different skills
- **Personality** (adventurous bonus / cautious conservative)

### 100-agent divergence experiment

```
ECO_AGENTS=100, run 150s:

Skill Buys (40 total, direction diverged):
  farmer:15  courier:14  doctor:5  miner:3  chef:2  engineer:1
  ← farmers buy farmer, couriers buy courier, few cross-investments (synergy works)

Contract Stats: total=2513  completed=1752  failed=761  volume=25578
  ← hiring happens at scale (skill-less agents hire); failures are real

Wealth divergence:
  Agent28(Chef) bal=9520   ← multi-skill investor becomes richest (compound/centralization)
  Agent60     bal=1882
  Henry/Alice/Agent61 = 0  ← bankrupt (failed investment / poor management)
```

**Conclusion**: facing the same economic opportunity, 100 agents made different choices — different professions tend to buy the matching skill, some hire heavily, some go bankrupt, some become the richest through skill investment. **Internal factors (personality / profession / capital) now shape economic behavior.**

> Scale experiments: the `ECO_AGENTS` env var (default 20, set to 100/200); profiles beyond 20 are generated by cycling profession / personality / capital pools.

## Skill System (M7)

A skill is an agent's **capability set**, deciding whether the agent can use a tool:

- **Skill decides "can it be used"** (skill isolation: an agent only sees the MCP tools its skills unlock)
- **Level decides "how well"** (skill level affects success rate / expected reward)

```
Agent
├── Skills          // capabilities: {engineer, trader}
│    ├── engineer Lv5
│    └── trader   Lv2
└── SkillLevels     // proficiency
```

**Skill isolation (the soul of Skill System)**: global tools → filtered by the agent's Skills → only the tools it "can see" reach the Planner:

```
Global Tools          Agent: Engineer        Agent: Courier
repair_machine   →    repair_machine    →    (not visible)
deliver_package  →    (not visible)     →    deliver_package
research_data    →    (not visible)     →    (not visible)
```

An agent can never call a tool outside its skills — this is a real Skill System, not hardcoded behavior.

### Decision chain (full MCP)

```
Goal
 ↓
Perception (economy state + my skills)
 ↓
Planner (skill-isolated filter + skill level + goal + personality trade-off)
 ↓
Decision.Action = claim <job>
 ↓
Executor → rt.CallTool("economy_machine", <tool>) → MCP Backend
 ↓
real result → Why (goal / economy / personality / skills / opportunity → therefore)
```

Phase-1 core tools (local mock backend returning `{success, reward}`, but the chain is real):

| Tool | Skill | Reward |
|---|---|---|
| `repair_machine` | Engineer | 30 |
| `deliver_package` | Courier | 15 |
| `research_data` | Researcher | 25 |

> Swap the mockBackend for a real MCP/HTTP service later; the interface stays the same.

### "Why" explains the skill

Click an agent → the decision rationale includes skills:

```
Goal: earn more money
Economy: balance 12 coins (wealth rank 16/20)
Personality: steady, likes stable income
Skills: engineer Lv7, trader Lv2
Opportunity: Repair Reactor(+40), Mine Ore(+35)
Therefore: I decided to take Repair Reactor (matches my skill)
```

![Agent Brain: Why + Skill System](screenshots/02-agent-brain-skill-system.png)

## 4 validation experiments

1. **Skill isolation**: an Engineer can never call `deliver_package`
2. **Goal affects skill choice**: Alice has Engineer+Trader; different goals (earn money vs repair) yield different choices
3. **Skill Level affects decisions**: Engineer Lv7 vs Lv1 differ in success rate / expected reward for the same task
4. **Why explains Skill**: Timeline shows "🔧 Alice used Engineer skill"; click for the full decision chain

## Single-file deploy (frontend embedded in the binary)

The frontend is embedded into the Go binary via `//go:embed`, so the whole world is **one executable / one container** — frontend and API are same-origin, no separate vite process.

### Option A: local single-file exe (Windows)

```powershell
# one-shot: build frontend (→ webstatic/dist) then go build embeds it
powershell -ExecutionPolicy Bypass -File worlds/economy/build.ps1
# outputs bin/economy.exe; run it then open http://localhost:19100
bin\economy.exe
```

### Option B: single-container deploy (recommended for the cloud)

```bash
# build from the repo root
docker build -t agentworld-economy -f worlds/economy/Dockerfile .

# run (frontend+API on the same port 19100)
docker run -p 19100:19100 -v economy-data:/data agentworld-economy
# open http://<cloud-server-ip>:19100
```

> The Dockerfile is a 3-stage build (node builds frontend → CGO_ENABLED=0 static Go build → alpine run),
> producing a **single binary containing the frontend assets**; the `economy.db` data volume persists under `/data`.

## Run (development)

```bash
# Backend (economy, :19100)
go run ./worlds/economy/cmd/economy

# Frontend (Economy Observatory, :5299, hot-reload)
cd worlds/economy/web && npm install && npm run dev
```

Env vars: `ECO_DB` (default economy.db), `ECO_INTERVAL` (3s), `ECO_TICK` (world demand refresh, 5s), `ECO_OBS_ADDR` (:19100), `ECO_AGENTS` (agent count, default 20, set 100/200 for scale experiments), `LLM_API_KEY` (optional; without it agents use the rule Planner).

## Observatory API

| Endpoint | Description |
|---|---|
| `GET /api/game` | Economy snapshot (assets / prices / open jobs / recent transactions / total wealth / **skill market** / **skill buys** / **labor market** / **contract stats**) |
| `GET /api/agents/{id}` | Agent deep state (balance / earn / spend / goal / personality / skills / why / inventory / **skill investment return**) |
| `GET /api/events` | Recent events |
| `GET /api/events/stream` | SSE realtime transaction stream |

## Layout

```
worlds/economy/
├── cmd/economy/main.go     # standalone entry (:19100, ECO_AGENTS for scaling)
├── economy/
│   ├── world.go            # world state: N agents + Skills + starting wealth
│   │                       #         + M5.1 level gates + M6.1 Service/Contract/Escrow
│   ├── economy.go          # economy ops: jobs / buy / sell / consume / transfers / hire / contracts
│   ├── perception.go       # perception: skill market (SkillOffer) + labor market (Services/WorkersBySkill) + scarcity
│   ├── capabilities.go     # MCP tool capability (mockBackend + Skill→Tool mapping + buy_skill)
├── module.go               # Planner: skill isolation + unified decision (Buy/Hire/Wait) + evaluate_* + Executor + Why
├── server.go               # economy observatory API + SSE + embedded frontend (go:embed)
├── webstatic/              # embedded frontend build output (//go:embed all:dist)
├── build.ps1               # one-shot single-file exe build (frontend embedded)
├── Dockerfile              # single-container deploy (node→go→alpine 3-stage)
└── web/                    # Economy Observatory (Vue3 + Vite)
```

## Reusable Skill infrastructure

`internal/skill/` provides a generic Skill Registry (`Register/Get/List/ToolsOf/AgentVisibleTools`), decoupled from any world. If the Skill System proves out, skills can be lifted from Economy into the AgentWorld Runtime / SDK layer so every world reuses them:

```
AgentWorld Runtime
       │
 Skill Registry
       │
  ┌────┼────┐
  ↓    ↓    ↓
GooseGame Economy Hotel
  │    │    │
Engineer Trader HotelOperator
  │    │    │
  └────┼────┘
       ↓
      MCP
```

## Related docs

- [README.md](README.md) — 中文版
- [README.md](../../README.md) — AgentWorld main docs
- [README_CN.md](../../README_CN.md) — AgentWorld main docs (Chinese)
