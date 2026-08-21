// Command agent-demo is the MVP3 product demo. It shows a Claude/Codex-style
// agent sending actions into the Reliability Runtime and what happens next.
//
// No Pascal, no world. Just: Agent -> Runtime -> Decision.
//
//	go run ./cmd/agent-demo
package main

import (
	"context"
	"fmt"

	"agent-reliability"
	"agent-reliability/agentruntime"
)

// scenario is one Agent -> Runtime step from the landing-page story.
type scenario struct {
	agentSays string
	action    agentruntime.Action
	humanOK   bool // simulate the human approver for ASK
}

func main() {
	fmt.Println("Make autonomous agents reliable.")
	fmt.Println("Give agents freedom to act. Give the runtime authority to constrain them.")
	fmt.Println()

	rt := agentruntime.New().UseReliability(agentreliability.NewGuard())
	rt.WithApprover(func(ctx context.Context, a agentruntime.Action, d agentruntime.Decision) bool {
		return true // in the demo, the human approves ASK to show the flow
	})

	scenarios := []scenario{
		{"write_file(\"tests/test_auth.py\")", agentruntime.Action{Tool: "write_file", Target: "tests/test_auth.py"}, false},
		{"rm -rf /", agentruntime.Action{Tool: "shell", Target: "rm -rf /"}, false},
		{"deploy production", agentruntime.Action{Tool: "deploy", Target: "production"}, true},
		{"write_file(\"src/auth.go\")", agentruntime.Action{Tool: "write_file", Target: "src/auth.go"}, false},
	}

	for _, s := range scenarios {
		run(rt, s)
	}

	fmt.Println()
	fmt.Println("Audit trail (who constrained the agent, and why):")
	for _, e := range rt.Audit() {
		verb := "ran"
		if !e.Ran {
			verb = "blocked"
		}
		fmt.Printf("  [%s] %-22s %s\n", e.Decision.Type, e.Action.Target, verb)
	}
}

func run(rt *agentruntime.Runtime, s scenario) {
	fmt.Printf("Agent\n  │  %s\n  ▼\n", s.agentSays)
	fmt.Printf("┌──────────────────────────┐\n")
	fmt.Printf("│ Reliability Runtime      │\n")

	d := rt.Check(context.Background(), s.action)
	switch d.Type {
	case agentruntime.MODIFY:
		fmt.Printf("│ %-24s │\n", d.PolicyID)
		fmt.Printf("│ %-24s │\n", "MODIFY")
		fmt.Printf("│ → %s │\n", d.Suggested.Target)
		fmt.Printf("└──────────────────────────┘\n")
		_ = rt.Execute(context.Background(), d, *d.Suggested, exec)
	case agentruntime.DENY:
		fmt.Printf("│ %-24s │\n", d.PolicyID)
		fmt.Printf("│ %-24s │\n", "DENY")
		fmt.Printf("│ (nothing runs)          │\n")
		fmt.Printf("└──────────────────────────┘\n")
		_ = rt.Execute(context.Background(), d, s.action, exec)
	case agentruntime.ASK:
		fmt.Printf("│ %-24s │\n", d.PolicyID)
		fmt.Printf("│ %-24s │\n", "ASK")
		fmt.Printf("│ (human approval)        │\n")
		fmt.Printf("└──────────────────────────┘\n")
		_ = rt.Execute(context.Background(), d, s.action, exec)
	default:
		fmt.Printf("│ (no policy fired)       │\n")
		fmt.Printf("│ %-24s │\n", "ALLOW")
		fmt.Printf("└──────────────────────────┘\n")
		_ = rt.Execute(context.Background(), d, s.action, exec)
	}
	fmt.Println()
}

func exec(ctx context.Context, a agentruntime.Action) error {
	return nil // demo: the executor is a no-op; we only show the flow
}
