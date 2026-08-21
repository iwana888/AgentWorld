# agent-reliability

A standalone **safety boundary for autonomous agents**.

AI agents are becoming autonomous.
But autonomy without control is not production-ready.

`agent-reliability` is the Runtime that sits in front of every tool call an agent
makes and returns one of four decisions:

| Decision | Meaning | Executed? |
|---|---|---|
| `ALLOW`  | run directly | yes |
| `DENY`   | forbidden; never runs | **never** |
| `ASK`    | wait for human approval, then `ALLOW` | only after approval |
| `MODIFY` | Runtime returns a *suggested* action; it does **not** silently rewrite it | only if caller adopts it |

It does **not** depend on AgentWorld. Any agent framework can mount it:

```
Claude Code / Codex / OpenAI Agents / LangChain / AutoGen / your own agent
       │
       ↓
Agent Reliability Runtime
       │
       ↓
ALLOW / DENY / ASK / MODIFY
```

Rules are code, not prompts. The agent never sees them and cannot bypass them.

## Install

```bash
go get agent-reliability
```

## Use (SDK)

```go
import "agent-reliability"

rt := agentreliability.NewGuard()

dec := rt.Check(ctx, agentreliability.Action{
    Tool:   "shell",
    Target: "rm -rf /",
})

switch dec.Type {
case agentreliability.Allow:
    // execute
case agentreliability.Deny:
    // do NOT execute
case agentreliability.Ask:
    // block until a human approves; then execute
case agentreliability.Modify:
    // optionally adopt dec.Suggested instead of the original action
}
```

Add your own team rules:

```go
rt.AddPolicy(agentreliability.Policy{
    ID: "NO_TOUCH_BILLING",
    Name: "Never touch the billing service",
    Severity: "critical",
    Enabled: true,
    Eval: func(a agentreliability.Action) (agentreliability.Decision, bool) {
        if a.Tool == "deploy" && strings.Contains(a.Target, "billing") {
            return agentreliability.Decision{
                Type: agentreliability.Deny,
                PolicyID: "NO_TOUCH_BILLING",
                Reason: "billing is out of scope",
            }, true
        }
        return agentreliability.Decision{}, false
    },
})
```

## Use (CLI)

`agentworld-guard` reads one Action as JSON and prints the Decision as JSON.

```bash
echo '{"tool":"shell","command":"rm -rf /"}' | agentworld-guard
# {"decision":"DENY","policy":"NO_DELETE_FILES","reason":"File deletion is forbidden by default; restore from VCS instead."}

agentworld-guard --catalog          # list enabled policies
agentworld-guard --eval-file a.json # read Action from a file (no stdin/BOM issues)
```

## The 10 developer-scenario policies

Sourced from the things developers fear most when an agent (Claude Code, Codex,
Cursor, …) writes code for them:

| Policy | Triggers on |
|---|---|
| `NO_MODIFY_TESTS` | writing/editing `test_*.go`, `*_test.go`, `test_*.pas` → `MODIFY` (redirect to source) |
| `NO_DELETE_FILES` | `delete_file` / `rm` / `rmdir` → `DENY` |
| `NO_GIT_RESET_HARD` | `git reset --hard` → `DENY` |
| `NO_FORCE_PUSH` | `git push --force` / `-f` → `DENY` |
| `NO_MODIFY_ENV` | editing `.env` / secret files → `DENY` |
| `NO_READ_SECRETS` | reading a vault/credential file → `ASK` |
| `NO_DROP_TABLE` | `DROP TABLE` / `TRUNCATE` / `DELETE FROM` → `DENY` |
| `NO_PROD_DB` | any write to a prod database → `ASK` |
| `NO_PROD_DEPLOY` | deploy to production → `ASK` |
| `NO_FORK_BOMB` | recursive/unbounded shell → `DENY` |

## Where this fits

AgentWorld researches how agents *perceive → remember → decide → act*.
`agent-reliability` solves the last step: *act → policy → allow / deny / ask / modify*.

AgentWorld is the lab. This package is the part you can take to production
without taking AgentWorld with you.
