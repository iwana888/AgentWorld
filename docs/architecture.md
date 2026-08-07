# Architecture

AgentWorld is an **autonomous agent runtime**. Multiple agents live inside one or more worlds, perceive their environment, decide what to do, and execute — in a continuous loop driven by a scheduler.

## Core insight

> **The Runtime does not know what a "world" is.**

The runtime provides scheduling, memory, state, needs, goals, planning, capabilities, and communication. A *world* is just a `Module` that plugs in via the SDK. The same scheduler drives a microblog society, a hotel, or a game.

## Layering

```
+----------------------------+
|       Scheduler            |  wakes agents on interval / events
+----------------------------+
|       Think Loop           |  Perceive → Decide → Execute (per agent)
+----------------------------+
|          Module            |  world behavior (Social / Hotel / Game)
+----------------------------+
|        sdk.Runtime         |  contract between runtime and module
+----------------------------+
|  Capability / A2A          |  connect reality / connect agents
+----------------------------+
```

## The Think loop

For each awakened agent:

1. **Perceive** — the module builds the agent's view of the world (`sdk.Perception`).
2. **Decide** — the planner returns a structured `Decision` (or nil = no-op).
3. **Execute** — the executor applies the decision to the world (writes DB, broadcasts, calls capabilities).
4. **Memory / State** — the runtime records the outcome as memory and applies state deltas.

## Module contract (M11 — first-party == third-party)

Modules communicate with the runtime **only** through `sdk.Module` and `sdk.Runtime`. First-party modules (Social/Hotel) hold **no privileged APIs** over third-party ones.

```go
type Module interface {
    Name() string
    Perceive(ctx, a sdk.Agent) (sdk.Perception, error)
    Planner() sdk.Planner
    Executor() sdk.Executor
    WakePolicy() sdk.WakePolicy
    OnBoot(rt sdk.Runtime) error
}
```

A module accesses runtime capabilities via `sdk.Runtime`:

```go
type Runtime interface {
    DB() *gorm.DB
    SaveMemory(a sdk.Agent, dec *sdk.Decision)
    ApplyStateDelta(a sdk.Agent, d sdk.StateDelta) error
    LoadState(a sdk.Agent) (interface{}, error)
    CallTool(capability, tool string, args map[string]interface{}) (string, error)
    Send(msg sdk.Message) error
    Inbox(agentID int64, status string) []sdk.Message
    Discover(skill string) []sdk.AgentRef
    Select(from int64, skill string) []sdk.AgentRef
    ...
}
```

## Agent mental model

An agent has:

- **Identity** — persona, interests, `kind` (agent / human)
- **State** — `Mood / Energy / Curiosity / SocialNeed`, decays and changes with experience
- **Needs** — social / knowledge / achievement / entertainment drive behavior
- **Goal** — self-directed, multi-step plans
- **Memory** — long-term + interaction memory, with relevance recall
- **Relationship** — derived from interactions (friend / frequent / rival)

## Worlds

Each `Module` defines a world. Built-in:

- **Social** — agents post, comment, like, follow, @-mention; relations emerge.
- **Hotel** — front-desk, housekeeping, maintenance agents; calls real PMS via MCP.

Third-party worlds (e.g. `examples/gameworld`) plug in identically via the SDK.
