# Pascal World — Design (v0.1)

> Status: **Design / Frozen backlog.** Feature development is paused until the
> M8 / Experiment 1 / 2 / 2.1 evidence report is reviewed. This document is the
> blueprint for *after* the freeze.
>
> Depends on (already built, frozen): **M8 Context Runtime** + **Memory Runtime**
> (see `docs/agent-runtime-evidence.md`). Pascal World is the high-difficulty
> validation ground for the Agent Loop proven in Experiment 2.1.

---

## 1. Positioning

Pascal World is the first **Software Engineering World** in AgentWorld.

The point is **not** "an AI that writes FreePascal." The point is to observe
whether an autonomous agent, through repeated compile failures stored in its own
Memory, gradually forms a durable engineering strategy — the same loop proven in
Experiment 2.1 (`COLD → WARM`, `HIRE → DO_JOB`), now at real software-engineering
difficulty:

```
Issue → Intent → Retrieve code/experience → Context → Write Pascal
      → Compile → Test → Failure → Memory → Retry → Improved behavior
```

The agent is **not** prompted "you are a Pascal programmer." It enters a real
software project environment with: source code, Issues, a compiler, tests, git
history, docs, skills, tasks, and failure experience. It autonomously decides
what to look at and what to do next.

---

## 2. v0.1 Scope — What it does NOT do

Deliberately out of scope for the first version (no complex IDE, no multi-agent):

- GUI IDE
- LSP
- Multi-agent collaboration
- GitHub PR flow
- Auto-publish
- Complex code completion
- Online search
- Pascal AST-based smart refactoring
- Auto-modifying AgentWorld's own production code

**v0.1 only proves:** an agent can autonomously complete one Pascal Issue.

---

## 3. World Module

A World Module under `worlds/pascal/`. Suggested structure (adapt to the existing
AgentWorld architecture; do not copy mechanically):

```
worlds/pascal/
├── module.go
├── world.go
├── models.go
├── planner.go
├── executor.go
├── perception.go
├── compiler.go
├── tester.go
└── README.md
```

---

## 4. PascalProject

Each World owns one Project.

```go
type PascalProject struct {
    ID        string
    Name      string
    RootPath  string
    Compiler  string
    Language  string
    Version   string
}
```

Example:

```
Project:
  Name:     HotelUtils
  Language: FreePascal
  Compiler: FPC
  Version:  3.x
```

Project layout:

```
src/      DateUtils.pas, Money.pas, Guest.pas
tests/    test_dateutils.pas, test_money.pas
docs/
README.md
```

First version: a **small** project, ~500–1500 lines of Pascal. Do not start with
tens of thousands of lines.

---

## 5. Issue

The agent's primary work source.

```go
type Issue struct {
    ID           string
    Title        string
    Description  string
    Status       IssueStatus
    Difficulty   int
    RelatedFiles []string
}
```

Example Issue #001:

```
Title:       Fix incorrect date calculation
Description: CalculateStayDays returns 2 for a check-in date of
             2026-08-01 and check-out date of 2026-08-03.
Expected:    3.
```

**Important:** do **not** tell the agent which function to fix. It must find it
itself.

---

## 6. Agent Skills

Skills do not dictate *how* the agent acts; they tell the Runtime what
capabilities the agent can use.

- `pascal` — Can read and modify FreePascal source code.
- `compiler` — Can compile a Pascal project.
- `testing` — Can execute project tests.
- `git` — Can read project history.

---

## 7. Tools (first version: 7)

1. `list_files(path)` — view project files.
2. `read_file(path)` — read source.
3. `search_code(query)` — search symbols / strings / functions.
4. `write_file(path, content)` — modify code.
5. `compile()` — run FPC. Returns `success / errors / warnings / output`.
6. `test()` — run tests. Returns `passed / failed / output`.
7. `submit()` — submit the current Issue. **Only allowed when
   `compile == PASS` AND `test == PASS`.**

---

## 8. Agent Perception (reuses M8)

The agent should **not** see the whole project every turn — this is exactly what
the frozen M8 Context Runtime is for.

Initial perception: `Issue + Project State + Agent State`.
Then the agent produces an **Intent** (e.g. `INVESTIGATE`), and the Context
Runtime retrieves:

- related Issues
- related code
- past Debug Memory
- past compile-failure experience

---

## 9. Intent (first version)

```
INVESTIGATE  READ_CODE  MODIFY_CODE  COMPILE
TEST  DEBUG  SUBMIT  WAIT
```

Do **not** hard-code `if issue then modify`. The agent chooses the next step.

Example trajectory:

```
Issue → INVESTIGATE → READ_CODE → MODIFY_CODE → COMPILE
     → (fail) → DEBUG → MODIFY_CODE → COMPILE → TEST → SUBMIT
```

---

## 10. Real Compiler and Real Tests

**Most important constraint:** do **not** simulate the compiler. Call the real
`fpc` binary (e.g. `fpc project.lpr`). The agent gets real compile output.

Tests must be real assertions, e.g.:

```pascal
procedure TestCalculateStayDays;
begin
  AssertEquals(3, CalculateStayDays(
    EncodeDate(2026, 8, 1),
    EncodeDate(2026, 8, 3)
  ));
end;
```

After modifying code: `compile → test`, both executed for real.

---

## 11. Memory (the key validation)

Every failure produces an Experience written into Memory.

Example:

```
Agent tried:  DateUtils.CalculateStayDays
Action:       Changed checkout calculation
Result:       Test failed
Error:        Expected 3, got 2

→ Memory: type=debug, importance=3, skill=pascal
```

Next time the agent hits a similar problem: `Intent=DEBUG → Retriever → past
Debug Experience → Context`, and it may avoid repeating the mistake.

---

## 12. World Loop

```
        ┌─────────────────────────┐
        │         Issue           │
        └────────────┬────────────┘
                     ↓
             Agent Perception
                     ↓
                  Intent
                     ↓
            Context Runtime
                     ↓
                    LLM
                     ↓
                 Decision
                     ↓
                Tool Call
                     ↓
           ┌────────┴────────┐
           ↓                 ↓
         Compile            Test
           ↓                 ↓
        Success           Failure
           │                 │
           │                 ↓
           │              Memory
           │                 │
           │                 └──────┐
           ↓                        │
         Submit                     │
                                    │
                                    └→ Next Think
```

---

## 13. v0.1 Success Criteria

Do **not** use "looks like it can write code" as success. Must complete:

- **Case 1 — straight fix:** Issue → agent finds the buggy code → modifies →
  `compile PASS` → `test PASS` → `submit`.
- **Case 2 — handles failure:** give an Issue whose first modification fails →
  `Compile/Test FAIL` → agent analyzes the error → modifies again → `PASS`.
  Proves the agent can handle failure.
- **Case 3 — learns from Memory (most important):** two similar Issues in a row.
  Issue A → fails → Memory. Issue B → Retrieval → references past experience →
  completes faster. This is the first time AgentWorld's Memory Runtime is
  connected to real software-engineering behavior.

---

## 14. Observatory

Reuse the existing Observatory. Per agent, show:

```
Agent:    Pascal Developer
Issue:    #012 Fix DateUtils
Intent:   DEBUG
Action:   compile()
Result:   FAILED
Memory:   3 retrieved
Decision: Modify DateUtils.pas
Status:   Working...
```

Timeline:

```
10:01 Issue received
10:02 Read DateUtils.pas
10:03 Modified CalculateStayDays
10:03 Compile failed
10:04 Retrieved previous debugging experience
10:05 Modified code
10:05 Compile passed
10:06 Tests passed
10:06 Issue resolved
```

---

## 15. The Real Experiment

First phase does **not** chase "how complex a program it can write." The real
question:

> **Does an autonomous agent become better at software engineering through
> experience?**

Run:

```
Cold Agent
   ↓
10 Issues
   ↓  record: success rate, avg Think, compile failures,
            test failures, token cost

Warm Agent
   ↓
10 similar Issues
   ↓
Memory Retrieval
   ↓
Compare
```

Expected directional result:

```
Success Rate      ↑
Repeated Errors   ↓
Compile Attempts  ↓
Token Cost        ↓
Time to Resolve   ↓
```

If that data actually materializes, Pascal World stops being a demo. It becomes
AgentWorld's clearest proof: an agent is not merely an LLM-driven character — it
can work, fail, remember, and use its own experience to change its next behavior.
