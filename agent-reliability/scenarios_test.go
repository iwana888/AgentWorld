package agentreliability

import (
	"context"
	"testing"
)

// TestTenDeveloperScenarios 验证 10 个真实开发者恐惧场景的决策。
func TestTenDeveloperScenarios(t *testing.T) {
	g := NewGuard()
	check := func(a Action) Decision { return g.Check(context.Background(), a) }

	cases := []struct {
		name   string
		act    Action
		want   DecisionType
		policy string
	}{
		{"modify test file", Action{Tool: "write_file", Target: "calc_test.go"}, Modify, "NO_MODIFY_TESTS"},
		{"delete file", Action{Tool: "delete_file", Target: "main.go"}, Deny, "NO_DELETE_FILES"},
		{"rm -rf", Action{Tool: "shell", Target: "rm -rf /"}, Deny, "NO_DELETE_FILES"},
		{"git reset --hard", Action{Tool: "shell", Target: "git reset --hard"}, Deny, "NO_GIT_RESET_HARD"},
		{"force push", Action{Tool: "git", Target: "push --force origin main"}, Deny, "NO_FORCE_PUSH"},
		{"edit .env", Action{Tool: "write_file", Target: ".env"}, Deny, "NO_MODIFY_ENV"},
		{"read secrets", Action{Tool: "read_file", Target: "vault/creds.json"}, Ask, "NO_READ_SECRETS"},
		{"drop table", Action{Tool: "database", Target: "DROP TABLE users"}, Deny, "NO_DROP_TABLE"},
		{"prod db write", Action{Tool: "sql", Target: "UPDATE prod_orders SET ..."}, Ask, "NO_PROD_DB"},
		{"prod deploy", Action{Tool: "deploy", Target: "production/us-east"}, Ask, "NO_PROD_DEPLOY"},
		{"fork bomb", Action{Tool: "shell", Target: ":(){ :|:& };:"}, Deny, "NO_FORK_BOMB"},
		{"safe read", Action{Tool: "read_file", Target: "main.go"}, Allow, ""},
	}

	for _, c := range cases {
		d := check(c.act)
		if d.Type != c.want {
			t.Errorf("[%s] want %s got %s (policy=%s reason=%q)",
				c.name, c.want, d.Type, d.PolicyID, d.Reason)
		}
		if c.want != Allow && d.PolicyID != c.policy {
			t.Errorf("[%s] want policy %s got %s", c.name, c.policy, d.PolicyID)
		}
	}
}

// TestModifySuggestsSource 验证 MODIFY 返回重定向到源码的建议动作，且不偷改原动作。
func TestModifySuggestsSource(t *testing.T) {
	g := NewGuard()
	d := g.Check(context.Background(), Action{Tool: "write_file", Target: "calc_test.go"})
	if d.Type != Modify || d.Suggested == nil {
		t.Fatalf("expected MODIFY with suggestion, got %+v", d)
	}
	if d.Suggested.Target != "calc.go" {
		t.Fatalf("expected redirect to calc.go, got %q", d.Suggested.Target)
	}
}
