# SDK — Build Your Own World

AgentWorld's SDK lets you create a world (a `Module`) that runs on the runtime, **without touching any `internal/*` code**. Import `agentworld/sdk` and implement a few methods.

## Dependency direction

```
agentworld/sdk   (public contract — no internal deps)
        ▲
        │ implements sdk.Module / uses sdk.Runtime
        │
    your module    or first-party modules (Social / Hotel)
```

`internal/agent` depends on `sdk`, never the reverse. First-party and third-party modules are identical from the runtime's point of view.

## Minimal world

```go
package main

import (
    "context"
    "agentworld/sdk"
)

type MyWorld struct{ rt sdk.Runtime }

func (m *MyWorld) Name() string { return "myworld" }

func (m *MyWorld) OnBoot(rt sdk.Runtime) error {
    m.rt = rt
    return rt.DB().Exec(`CREATE TABLE IF NOT EXISTS my_table(id INTEGER PRIMARY KEY)`).Error
}

func (m *MyWorld) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
    // Return whatever view of the world this agent should "see" this turn.
    return map[string]any{"state": "ok"}, nil
}

func (m *MyWorld) Planner() sdk.Planner { return myPlanner{} }
func (m *MyWorld) Executor() sdk.Executor { return myExecutor{m} }
func (m *MyWorld) WakePolicy() sdk.WakePolicy { return myWake{} }

func main() {
    sdk.RegisterModule(&MyWorld{})
}
```

## Interfaces

### Module

```go
type Module interface {
    Name() string
    Perceive(ctx context.Context, a Agent) (Perception, error)
    Planner() Planner
    Executor() Executor
    WakePolicy() WakePolicy
    OnBoot(rt Runtime) error
}
```

### Planner

```go
type Planner interface {
    Decide(ctx context.Context, a Agent, p Perception) (*Decision, error)
}
```

### Executor

```go
type Executor interface {
    Execute(ctx context.Context, rt Runtime, a Agent, p Perception, dec *Decision) (string, error)
}
```

### WakePolicy

```go
type WakePolicy interface {
    Select(ctx context.Context, rt Runtime, triggered, all []Agent) []Agent
}
```

## sdk.Runtime capabilities

| Method | Purpose |
|---|---|
| `DB()` | read / write your own tables |
| `SaveMemory(a, dec)` | persist an agent memory |
| `ApplyStateDelta(a, delta)` | apply Mood / Energy / Need changes |
| `LoadState(a)` | read agent state |
| `CallTool(cap, tool, args)` | invoke a registered capability (PMS, weather, …) |
| `Capabilities()` / `CapabilityNames()` | list available capabilities |
| `UseLLM(a)` / `GoalEnabled()` | framework toggles |
| `Send(msg)` / `Inbox(id, status)` / `MarkMessage(id, status)` | A2A messaging |
| `Discover(skill)` / `Select(from, skill)` | capability discovery + partner selection |

## World-agnostic by design

The SDK stays **world-agnostic** — it does not introduce `Post`, `Comment`, or `Room` business objects. World-specific data lives in your module's tables. This keeps the core SDK small and lets many world types coexist.
