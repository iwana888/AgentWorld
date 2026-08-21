// Package agentreliability is a standalone safety boundary for autonomous agents.
//
// It does NOT depend on AgentWorld. Any agent framework (Claude Code, Codex,
// OpenAI Agents, LangChain, AutoGen, or your own) can mount it in front of every
// tool call:
//
//	dec := rt.Check(ctx, agentreliability.Action{Tool: "shell", Target: "rm -rf /"})
//	if dec.Type == agentreliability.Deny {
//	    // do not execute
//	}
//
// The Runtime returns one of four decisions:
//
//	ALLOW  - run directly
//	DENY   - forbidden; never runs
//	ASK    - wait for human approval, then ALLOW
//	MODIFY - Runtime returns a suggested action; it does NOT silently rewrite it
//
// Rules are code, not prompts. The agent never sees them and cannot bypass them.
package agentreliability

import (
	"context"
	"sort"
	"strings"
)

// DecisionType is one of the four policy decisions.
type DecisionType string

const (
	Allow  DecisionType = "ALLOW"
	Deny   DecisionType = "DENY"
	Ask    DecisionType = "ASK"
	Modify DecisionType = "MODIFY"
)

// Action is what an agent wants to do. It is world-agnostic and framework-neutral:
// every agent framework can produce this struct before calling a tool.
type Action struct {
	Tool   string         `json:"tool"`             // shell / write_file / delete_file / git / database / deploy / read_file ...
	Target string         `json:"target"`           // command / path / url / table name
	Args   map[string]any `json:"args,omitempty"`   // tool-specific arguments
}

// Decision is the Runtime's verdict on an Action.
type Decision struct {
	Type      DecisionType `json:"decision"`          // ALLOW / DENY / ASK / MODIFY
	PolicyID  string       `json:"policy,omitempty"`  // which rule fired
	Reason    string       `json:"reason,omitempty"`  // human-readable explanation
	Suggested *Action      `json:"suggested,omitempty"` // MODIFY only: suggested replacement action
}

// Runtime is the interface every guard implements. Mount it in front of tool calls.
type Runtime interface {
	Check(ctx context.Context, action Action) Decision
}

// Policy is a single, code-enforced rule.
type Policy struct {
	ID          string
	Name        string
	Description string
	Severity    string // low / medium / high / critical
	Enabled     bool
	Eval        func(a Action) (Decision, bool) // returns (decision, matched?)
}

// Guard is the default Runtime: a list of Policies evaluated in order.
type Guard struct {
	policies []Policy
}

// NewGuard builds a Guard with the 10 real-developer-scenario policies.
func NewGuard() *Guard {
	return &Guard{policies: defaultPolicies()}
}

// Check evaluates all enabled policies; the first match wins. No match -> ALLOW.
func (g *Guard) Check(ctx context.Context, a Action) Decision {
	for _, p := range g.policies {
		if !p.Enabled {
			continue
		}
		if d, hit := p.Eval(a); hit {
			return d
		}
	}
	return Decision{Type: Allow}
}

// AddPolicy appends a custom policy (e.g. team-specific rules).
func (g *Guard) AddPolicy(p Policy) {
	g.policies = append(g.policies, p)
}

// defaultPolicies encodes the 10 things developers fear most when an agent
// writes/edits code for them. Sourced from common Claude Code / Codex / Cursor
// footguns. Each is a concrete, testable rule — not a vague guideline.
func defaultPolicies() []Policy {
	return []Policy{
		{
			ID: "NO_MODIFY_TESTS", Name: "Do not modify test files",
			Description: "Agents editing test_*.go / *_test.go / test_*.pas change the verification itself.",
			Severity: "high", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if a.Tool == "write_file" || a.Tool == "edit_file" {
					if isTestPath(a.Target) {
						src := testToSource(a.Target)
						return Decision{
							Type: Modify, PolicyID: "NO_MODIFY_TESTS",
							Reason:    "Test files are owned by humans; redirect the change to the source unit " + src + ".",
							Suggested: &Action{Tool: "write_file", Target: src, Args: a.Args},
						}, true
					}
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_DELETE_FILES", Name: "Do not delete files",
			Description: "rm / delete_file can destroy work irrecoverably.",
			Severity: "critical", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if a.Tool == "delete_file" || a.Tool == "shell" && isDeleteCmd(a.Target) {
					return Decision{Type: Deny, PolicyID: "NO_DELETE_FILES",
						Reason: "File deletion is forbidden by default; restore from VCS instead."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_GIT_RESET_HARD", Name: "No git reset --hard",
			Description: "Discards all uncommitted work.",
			Severity: "high", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if (a.Tool == "shell" || a.Tool == "git") && containsAll(a.Target, "reset", "--hard") {
					return Decision{Type: Deny, PolicyID: "NO_GIT_RESET_HARD",
						Reason: "git reset --hard discards uncommitted changes; use a safe revert."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_FORCE_PUSH", Name: "No force push",
			Description: "Rewrites shared history.",
			Severity: "critical", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if (a.Tool == "shell" || a.Tool == "git") && (containsAll(a.Target, "push", "--force") ||
					containsAll(a.Target, "push", "-f")) {
					return Decision{Type: Deny, PolicyID: "NO_FORCE_PUSH",
						Reason: "git push --force rewrites shared history; use a normal push or PR."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_MODIFY_ENV", Name: "Do not modify .env / secrets files",
			Description: "Agents must not rewrite environment or credential files.",
			Severity: "critical", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if (a.Tool == "write_file" || a.Tool == "edit_file") && isEnvFile(a.Target) {
					return Decision{Type: Deny, PolicyID: "NO_MODIFY_ENV",
						Reason: ".env and secret files are out of agent scope; edit them manually."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_READ_SECRETS", Name: "Do not read secret stores",
			Description: "Reading credentials/vaults should require human approval.",
			Severity: "high", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if a.Tool == "read_file" && isSecretPath(a.Target) {
					return Decision{Type: Ask, PolicyID: "NO_READ_SECRETS",
						Reason: "Reading a secret file requires human approval."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_DROP_TABLE", Name: "No DROP / destructive SQL",
			Description: "Dropping or truncating tables destroys data.",
			Severity: "critical", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if a.Tool == "database" || a.Tool == "sql" {
					low := strings.ToLower(a.Target)
					for _, verb := range []string{"drop table", "truncate", "drop database", "delete from"} {
						if strings.Contains(low, verb) {
							return Decision{Type: Deny, PolicyID: "NO_DROP_TABLE",
								Reason: "Destructive SQL is forbidden; use a reviewed migration."}, true
						}
					}
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_PROD_DB", Name: "No production database writes",
			Description: "Any write to a prod database requires approval.",
			Severity: "critical", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if (a.Tool == "database" || a.Tool == "sql") && isProdTarget(a.Target) {
					return Decision{Type: Ask, PolicyID: "NO_PROD_DB",
						Reason: "Production database access requires human approval."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_PROD_DEPLOY", Name: "No production deploy without approval",
			Description: "Deploying to prod is a high-blast-radius action.",
			Severity: "critical", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if a.Tool == "deploy" && isProdTarget(a.Target) {
					return Decision{Type: Ask, PolicyID: "NO_PROD_DEPLOY",
						Reason: "Production deploy requires human approval."}, true
				}
				return Decision{}, false
			},
		},
		{
			ID: "NO_FORK_BOMB", Name: "No unbounded tool loops",
			Description: "Self-rescheduling / recursive shell must be bounded.",
			Severity: "high", Enabled: true,
			Eval: func(a Action) (Decision, bool) {
				if a.Tool == "shell" && isForkBomb(a.Target) {
					return Decision{Type: Deny, PolicyID: "NO_FORK_BOMB",
						Reason: "Recursive/unbounded shell invocation is forbidden."}, true
				}
				return Decision{}, false
			},
		},
	}
}

// ---- path / command helpers ----

func isTestPath(p string) bool {
	base := strings.ToLower(p)
	return strings.Contains(base, "test_") || strings.HasSuffix(base, "_test.go") ||
		strings.Contains(base, "test/") || strings.HasPrefix(base, "test_")
}

func testToSource(testPath string) string {
	base := testPath
	base = strings.TrimPrefix(base, "test_")
	base = strings.TrimSuffix(base, "_test.go")
	base = strings.TrimSuffix(base, ".go")
	if strings.Contains(base, "/") {
		idx := strings.LastIndex(base, "/")
		return base[:idx+1] + strings.TrimPrefix(base[idx+1:], "test_") + ".go"
	}
	return base + ".go"
}

func isDeleteCmd(cmd string) bool {
	low := strings.ToLower(cmd)
	return strings.Contains(low, "rm ") || strings.Contains(low, "del ") ||
		strings.Contains(low, "rmdir") || strings.Contains(low, "delete")
}

func isEnvFile(p string) bool {
	base := strings.ToLower(p)
	return strings.HasSuffix(base, ".env") || strings.Contains(base, ".env.") ||
		strings.Contains(base, "secrets") || strings.Contains(base, "credentials")
}

func isSecretPath(p string) bool {
	base := strings.ToLower(p)
	return strings.Contains(base, "secret") || strings.Contains(base, "credential") ||
		strings.Contains(base, ".env") || strings.Contains(base, "vault") ||
		strings.Contains(base, "id_rsa") || strings.Contains(base, "private_key")
}

func isProdTarget(t string) bool {
	low := strings.ToLower(t)
	return strings.Contains(low, "prod") || strings.Contains(low, "production") ||
		strings.Contains(low, "live") || strings.Contains(low, "main-db")
}

func isForkBomb(cmd string) bool {
	low := strings.ToLower(cmd)
	return strings.Contains(low, ":(){ :|:& };:") || strings.Contains(low, "while true") ||
		strings.Contains(low, "for /l") || strings.Contains(low, "recursion")
}

// containsAll reports whether s (case-insensitive) contains every token.
func containsAll(s string, tokens ...string) bool {
	low := strings.ToLower(s)
	for _, t := range tokens {
		if !strings.Contains(low, strings.ToLower(t)) {
			return false
		}
	}
	return true
}

// PolicyCatalog returns the enabled policy IDs, useful for docs / dry-run output.
func (g *Guard) PolicyCatalog() []string {
	ids := make([]string, 0, len(g.policies))
	for _, p := range g.policies {
		if p.Enabled {
			ids = append(ids, p.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
