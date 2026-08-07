# Writing a Module

A **Module** is a world. It defines how agents perceive, decide, execute, and get awakened inside that world. The runtime treats every module identically.

## Structure of a module

A module is typically a Go package that implements `sdk.Module` and can carry its own state, tables, and logic. It accesses shared runtime services through `sdk.Runtime` only.

## The lifecycle

1. **OnBoot** — called once at startup. Seed data, create tables, register capabilities.
2. **Perceive** — per agent, per turn. Build the agent's view.
3. **Planner.Decide** — per agent. Return a `Decision` or nil.
4. **Executor.Execute** — per agent. Apply the decision to the world.

## Example: a "research" world

```go
type ResearchWorld struct{ rt sdk.Runtime }

func (w *ResearchWorld) Name() string { return "research" }

func (w *ResearchWorld) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
    // What does this researcher see today?
    return researchView{Topic: "embeddings"}, nil
}

func (w *ResearchWorld) Planner() sdk.Planner { return researchPlanner{} }
func (w *ResearchWorld) Executor() sdk.Executor { return researchExecutor{w} }
func (w *ResearchWorld) WakePolicy() sdk.WakePolicy { return allWakePolicy{} }
```

## Using capabilities

A module can call registered capabilities (MCP / HTTP tools):

```go
out, err := rt.CallTool("weather", "get_weather", map[string]interface{}{})
```

## Using A2A

Agents in your world can discover and message agents in other worlds:

```go
// Find partners who can plan travel
candidates := rt.Select(me.AgentID, "travel.plan.v1")
if len(candidates) > 0 {
    best := candidates[0]
    rt.Send(sdk.Message{From: me.AgentID, To: best.AgentID, Intent: "travel.plan.v1", Payload: map[string]interface{}{"city": "Shanghai"}})
}

// Read my inbox
msgs := rt.Inbox(me.AgentID, sdk.MsgStatusPending)
```

## Good practices

- Keep your world's data in your own tables (`rt.DB()`).
- Do not import `internal/*` — the SDK contract is enough.
- Return a `Perception` that carries only what the agent needs for this turn.
- Let relationships and selection emerge from communication, not from configuration.
