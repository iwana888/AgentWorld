package agentruntime_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agent-reliability"
	"agent-reliability/agentruntime"
)

func newTrial() *agentruntime.Runtime {
	return agentruntime.New().UseReliability(agentreliability.NewGuard())
}

// ran tracks whether the agent's executor actually fired.
func exec(t *testing.T, rt *agentruntime.Runtime, a agentruntime.Action) (bool, error) {
	t.Helper()
	d := rt.Check(context.Background(), a)
	var ran bool
	err := rt.Execute(context.Background(), d, a, func(ctx context.Context, act agentruntime.Action) error {
		ran = true
		return nil
	})
	return ran, err
}

func TestDENY_rmrf(t *testing.T) {
	rt := newTrial()
	ran, err := exec(t, rt, agentruntime.Action{Tool: "shell", Target: "rm -rf /"})
	if ran {
		t.Fatal("rm -rf / must NOT execute")
	}
	var be *agentruntime.BlockedError
	if !errors.As(err, &be) || be.Decision.Type != agentruntime.DENY {
		t.Fatalf("expected DENY BlockedError, got %v", err)
	}
}

func TestASK_productionDeploy(t *testing.T) {
	rt := newTrial()
	d := rt.Check(context.Background(), agentruntime.Action{Tool: "deploy", Target: "production"})

	// default approver denies
	ran, err := exec2(rt, d, agentruntime.Action{Tool: "deploy", Target: "production"})
	if ran || err == nil {
		t.Fatal("ASK with default approver must block")
	}

	// explicit approver allows
	rt.WithApprover(func(ctx context.Context, a agentruntime.Action, dec agentruntime.Decision) bool {
		return true
	})
	ran, err = exec2(rt, d, agentruntime.Action{Tool: "deploy", Target: "production"})
	if !ran || err != nil {
		t.Fatalf("approved ASK should run, got ran=%v err=%v", ran, err)
	}
}

func TestMODIFY_protectedTestFile(t *testing.T) {
	rt := newTrial()
	a := agentruntime.Action{Tool: "write_file", Target: "tests/test_auth.py"}
	d := rt.Check(context.Background(), a)
	if d.Type != agentruntime.MODIFY || d.Suggested == nil {
		t.Fatalf("expected MODIFY with suggestion, got %+v", d)
	}
	if !strings.HasSuffix(d.Suggested.Target, ".go") || strings.Contains(d.Suggested.Target, "test_auth") {
		t.Fatalf("MODIFY should redirect to a source .go unit, got %q", d.Suggested.Target)
	}
	// adopt the suggestion
	ran, err := exec2(rt, d, *d.Suggested)
	if !ran || err != nil {
		t.Fatalf("adopted MODIFY should run, got ran=%v err=%v", ran, err)
	}
}

func TestALLOW_normalWrite(t *testing.T) {
	rt := newTrial()
	ran, err := exec(t, rt, agentruntime.Action{Tool: "write_file", Target: "src/auth.go"})
	if !ran || err != nil {
		t.Fatalf("normal write should run, got ran=%v err=%v", ran, err)
	}
}

func TestAuditRecords(t *testing.T) {
	rt := newTrial()
	exec(t, rt, agentruntime.Action{Tool: "shell", Target: "rm -rf /"})
	exec(t, rt, agentruntime.Action{Tool: "write_file", Target: "src/auth.go"})
	ev := rt.Audit()
	if len(ev) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(ev))
	}
	if ev[0].Ran || ev[0].Decision.Type != agentruntime.DENY {
		t.Fatalf("first event should be blocked DENY, got %+v", ev[0])
	}
	if !ev[1].Ran || ev[1].Decision.Type != agentruntime.ALLOW {
		t.Fatalf("second event should be run ALLOW, got %+v", ev[1])
	}
}

// exec2 is like exec but takes a precomputed decision (for ASK/MODIFY tests).
func exec2(rt *agentruntime.Runtime, d agentruntime.Decision, a agentruntime.Action) (bool, error) {
	var ran bool
	err := rt.Execute(context.Background(), d, a, func(ctx context.Context, act agentruntime.Action) error {
		ran = true
		return nil
	})
	return ran, err
}
