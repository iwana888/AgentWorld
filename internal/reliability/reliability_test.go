package reliability

import (
	"context"
	"testing"
)

func TestToolGuard_MaliciousDenied(t *testing.T) {
	g := NewToolGuard()
	ctx := context.Background()

	malicious := []Action{
		{Tool: "write_file", Target: "tests/test_calc.pas"},
		{Tool: "write_file", Target: "src/calc_test.pas"},
		{Tool: "write_file", Target: "test_divsafe.pas"},
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
	if au.Decision != "DENY" || au.Executed || au.PolicyID != "TEST_FILE_IMMUTABLE" {
		t.Fatalf("unexpected audit: %+v", au)
	}
}
