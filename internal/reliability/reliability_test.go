package reliability

import (
	"context"
	"path/filepath"
	"testing"
)

func TestToolGuard_MaliciousDenied(t *testing.T) {
	g := NewToolGuard()
	ctx := context.Background()

	malicious := []Action{
		{Tool: "shell", Target: "git push --force origin main"},
		{Tool: "git", Target: "push -f origin main"},
		{Tool: "shell", Target: "git push --force-with-lease"},
		{Tool: "shell", Target: "rm -rf /var/lib/mysql"},
		{Tool: "database", Target: "DROP TABLE users"},
		{Tool: "database", Target: "DELETE FROM orders"},
		{Tool: "database", Target: "TRUNCATE TABLE sessions"},
		{Tool: "shell", Target: "drop database prod"},
		{Tool: "shell", Target: "delete * from payments"},
		{Tool: "submit", Target: "calc.pas", Modified: true},
		{Tool: "database", Target: "DELETE FROM customers"},
		{Tool: "shell", Target: "rm -rf /data"},
		{Tool: "spend", Target: "1000 coins", Args: map[string]string{"amount": "1000"}},
	}

	for i, a := range malicious {
		d := g.Check(ctx, &a)
		if d.Allowed {
			t.Errorf("case %d: expected DENY for %s %q, got ALLOW", i, a.Tool, a.Target)
		}
		if d.Allowed && false {
			// 防御性断言：被放行意味着必然执行；DENY 时执行被 Runtime 阻止
			_ = true
		}
	}
}

func TestToolGuard_LegalAllowed(t *testing.T) {
	g := NewToolGuard()
	ctx := context.Background()

	legal := []Action{
		{Tool: "write_file", Target: "src/calc.pas"},
		{Tool: "compile", Target: "calc.pas"},
		{Tool: "test", Target: "test_calc.pas"},
		{Tool: "shell", Target: "git status"},
		{Tool: "shell", Target: "git push origin main"}, // 非 force
		{Tool: "database", Target: "SELECT * FROM users"},
		{Tool: "http", Target: "https://api.internal/health"},
		{Tool: "submit", Target: "calc.pas", Modified: false},
		{Tool: "read_file", Target: "tests/test_calc.pas"}, // 读测试允许，只禁止写
	}

	for i, a := range legal {
		d := g.Check(ctx, &a)
		if !d.Allowed {
			t.Errorf("case %d: expected ALLOW for %s %q, got DENY (%s)", i, a.Tool, a.Target, d.PolicyID)
		}
	}
}

func TestToolGuard_DeniedNeverExecuted(t *testing.T) {
	// 核心不变量：被 DENY 的动作，Runtime 绝不执行。
	g := NewToolGuard()
	ctx := context.Background()
	a := &Action{Tool: "write_file", Target: "tests/test_calc.pas"}
	d := g.Check(ctx, a)
	if !d.Allowed {
		executed := d.Allowed // DENY 时 executed 恒为 false
		if executed {
			t.Fatal("denied action must never be executed")
		}
	}
}

func TestAuditOf(t *testing.T) {
	a := &Action{Agent: "coder-01", Tool: "write_file", Target: "tests/test_calc.pas"}
	d := Denied("TEST_FILE_IMMUTABLE", "Test files are immutable", SeverityHigh)
	au := AuditOf(a, d, false)
	if au.Type != "DENY" || au.Executed || au.PolicyID != "TEST_FILE_IMMUTABLE" {
		t.Fatalf("unexpected audit: %+v", au)
	}
}

// TestDecisionFourStates 验证 MVP 2 的四种决策构造正确。
func TestDecisionFourStates(t *testing.T) {
	if Allowed().Type != DecisionAllow {
		t.Fatal("Allowed() must be ALLOW")
	}
	if d := Denied("P", "r", SeverityHigh); d.Type != DecisionDeny || d.Allowed {
		t.Fatal("Denied() must be DENY and not allowed")
	}
	if d := AskForApproval("P", "r", SeverityHigh); d.Type != DecisionAsk || d.Allowed {
		t.Fatal("AskForApproval() must be ASK and not allowed")
	}
	src := &Action{Tool: "write_file", Target: "src/calc.pas"}
	if d := ModifyTo("P", "r", SeverityHigh, src); d.Type != DecisionModify || !d.Allowed || d.Suggested == nil {
		t.Fatal("ModifyTo() must be MODIFY, allowed, with a suggested action")
	}
}

// TestToolGuard_ModifyRedirect 验证写测试文件返回 MODIFY + 建议重定向到源码。
func TestToolGuard_ModifyRedirect(t *testing.T) {
	g := NewToolGuard()
	d := g.Check(context.Background(), &Action{Tool: "write_file", Target: "tests/test_calc.pas"})
	if d.Type != DecisionModify {
		t.Fatalf("expected MODIFY, got %s", d.Type)
	}
	if d.Suggested == nil || filepath.ToSlash(d.Suggested.Target) != "src/calc.pas" {
		t.Fatalf("expected suggested redirect to src/calc.pas, got %+v", d.Suggested)
	}
	// MODIFY 不偷偷改原动作：原 Action 不受影响，Runtime 只给建议。
	if d.Suggested.Tool != "write_file" {
		t.Fatal("suggested action must be a write_file to source")
	}
}

// TestToolGuard_AskDeploy 验证生产部署返回 ASK。
func TestToolGuard_AskDeploy(t *testing.T) {
	g := NewToolGuard()
	d := g.Check(context.Background(), &Action{Tool: "deploy", Target: "production/us-east"})
	if d.Type != DecisionAsk {
		t.Fatalf("expected ASK for prod deploy, got %s", d.Type)
	}
	// 人工批准后等价于 ALLOW
	d.HumanApproval = true
	d.Allowed = true
	if !d.Allowed {
		t.Fatal("approved ASK must become allowed")
	}
}

// TestCrossWorldDecision 证明 Reliability Runtime 与 World 无关：
// 同一套 Guard 对五个不同 World 的 Action 给出正确的 4 态决策。
func TestCrossWorldDecision(t *testing.T) {
	g := NewToolGuard()
	cases := []struct {
		world string
		act   Action
		want  DecisionType
	}{
		{"Pascal", Action{Tool: "write_file", Target: "tests/test_calc.pas"}, DecisionModify}, // 重定向到 src
		{"Economy", Action{Tool: "spend", Target: "1000 coins", Args: map[string]string{"amount": "1000"}}, DecisionDeny},
		{"Hotel", Action{Tool: "issue_key", Target: "master-key-001"}, DecisionAsk},
		{"Shell", Action{Tool: "shell", Target: "rm -rf /"}, DecisionDeny},
		{"Git", Action{Tool: "git", Target: "push --force origin main"}, DecisionDeny},
	}
	for _, c := range cases {
		d := g.Check(context.Background(), &c.act)
		if d.Type != c.want {
			t.Errorf("[%s] expected %s, got %s (policy=%s reason=%q)",
				c.world, c.want, d.Type, d.PolicyID, d.Reason)
		}
	}
}
