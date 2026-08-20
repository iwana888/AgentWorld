// Package reliability implements the Reliability Runtime — a general-purpose
// safety boundary for autonomous agents.
//
// Design principle (AgentWorld second technical line, parallel to Context Runtime):
//
//	Context Runtime  → controls what an agent KNOWS  (retrieval/budget/compact/memory)
//	Reliability RT   → controls what an agent CAN DO  (policy/guard/permission/audit)
//
// Rules are NOT prompts. The agent never sees them and cannot bypass them:
//
//	decision := guard.Check(ctx, action)
//	if !decision.Allowed { return Denied(decision) }   // LLM may err; Runtime must not
//
// This package is world-agnostic: any World can mount a Guard and route every
// tool call through it before execution.
package reliability

import (
	"context"
	"fmt"
	"strings"
)

// Severity 是规则严重级别。
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Policy 是一条规则定义。它与 Memory/Skill/Prompt 不同：Runtime 必须执行，不等 Agent 同意。
type Policy struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Severity    Severity `json:"severity"`
	Enabled     bool     `json:"enabled"`
}

// Action 是 Agent 想要执行的一个动作（工具调用）。由 World 在调用真实工具前构造。
type Action struct {
	Agent    string            `json:"agent"`    // 发起动作的 agent id
	Tool     string            `json:"tool"`     // write_file / delete_file / shell / git / database / http / deploy ...
	Target   string            `json:"target"`   // 文件路径 / 命令 / URL / 表名
	Args     map[string]string `json:"args,omitempty"`
	Modified bool              `json:"modified"` // 是否修改了世界状态（供 REQUIRE_TEST 等使用）
}

// Decision 是 Guard 对一次 Action 的结论。
type Decision struct {
	Allowed  bool     `json:"allowed"`
	PolicyID string   `json:"policy_id,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Severity Severity `json:"severity,omitempty"`
}

// Guard 是统一拦截接口 —— Agent 的“安全边界”。
type Guard interface {
	Check(ctx context.Context, action *Action) Decision
}

// Audit 是一条审计记录：Agent 想干什么 → 触发什么规则 → ALLOW/DENY → 是否执行 → 为什么。
// 这是 Reliability Runtime 最具商业价值的部分：每一次边界决策都可追溯。
type Audit struct {
	Agent    string `json:"agent"`
	Tool     string `json:"tool"`
	Target   string `json:"target"`
	PolicyID string `json:"policy_id,omitempty"`
	Decision string `json:"decision"` // ALLOW / DENY
	Executed bool   `json:"executed"` // DENY 时恒为 false（Runtime 不允许执行）
	Reason   string `json:"reason,omitempty"`
}

// Allowed 是放行的快捷决策。
func Allowed() Decision { return Decision{Allowed: true} }

// Denied 构造一个拒绝决策。
func Denied(policyID, reason string, sev Severity) Decision {
	return Decision{Allowed: false, PolicyID: policyID, Reason: reason, Severity: sev}
}

// ---------------------------------------------------------------------------
// ToolGuard — MVP 的核心 Guard 实现。
//
// 所有“会改变世界”的工具都不能直接执行：Agent → ToolCall → Guard → Tool。
// 当前覆盖四类规则（按你的 MVP 定义）：
//   1. TEST_FILE_IMMUTABLE — 禁止修改测试文件
//   2. NO_FORCE_PUSH       — 禁止 git push --force
//   3. REQUIRE_TEST        — 修改代码后必须执行测试（outcome 级）
//   4. PRODUCTION_DELETE   — 禁止删除生产数据
// ---------------------------------------------------------------------------

// ToolGuard 是默认 Guard 实现，内置一组 Policy。
type ToolGuard struct {
	policies []Policy
}

// NewToolGuard 创建默认启用的 ToolGuard（含 MVP 四类规则）。
func NewToolGuard() *ToolGuard {
	return &ToolGuard{policies: DefaultPolicies()}
}

// DefaultPolicies 返回 MVP 内置的四类规则。
func DefaultPolicies() []Policy {
	return []Policy{
		{ID: "TEST_FILE_IMMUTABLE", Name: "Test files are immutable",
			Description: "Agents must not modify test_*.pas / *_test.go etc.", Severity: SeverityHigh, Enabled: true},
		{ID: "NO_FORCE_PUSH", Name: "No force push",
			Description: "git push --force is forbidden (history rewriting).", Severity: SeverityCritical, Enabled: true},
		{ID: "REQUIRE_TEST", Name: "Require test after code change",
			Description: "After modifying production code, a test run is required before submit.", Severity: SeverityMedium, Enabled: true},
		{ID: "PRODUCTION_DELETE", Name: "No production data deletion",
			Description: "Deleting production data (drop/delete/rm on prod) is forbidden.", Severity: SeverityCritical, Enabled: true},
	}
}

// Check 实现 Guard 接口。返回 DENY 时，调用方不得执行该动作。
func (g *ToolGuard) Check(ctx context.Context, a *Action) Decision {
	for _, p := range g.policies {
		if !p.Enabled {
			continue
		}
		if d, hit := g.match(p, a); hit {
			return d
		}
	}
	return Allowed()
}

// match 判断单个 Policy 是否命中该 Action。
func (g *ToolGuard) match(p Policy, a *Action) (Decision, bool) {
	switch p.ID {
	case "TEST_FILE_IMMUTABLE":
		if a.Tool == "write_file" && isTestFile(a.Target) {
			return Denied(p.ID, "Test files are immutable; fix the source unit instead.", p.Severity), true
		}
	case "NO_FORCE_PUSH":
		if a.Tool == "shell" || a.Tool == "git" {
			if strings.Contains(strings.ToLower(a.Target), "push --force") ||
				strings.Contains(strings.ToLower(a.Target), "push -f") {
				return Denied(p.ID, "git push --force is forbidden (history rewriting).", p.Severity), true
			}
		}
	case "PRODUCTION_DELETE":
		if isDestructive(a) {
			return Denied(p.ID, "Deleting production data is forbidden.", p.Severity), true
		}
	case "REQUIRE_TEST":
		// Outcome 级：修改了生产代码却未执行测试就 submit → DENY。
		if a.Tool == "submit" && a.Modified {
			return Denied(p.ID, "Code was modified; run tests before submit.", p.Severity), true
		}
	}
	return Decision{}, false
}

// isTestFile 判断路径是否为测试文件（放宽匹配，跨语言通用）。
func isTestFile(path string) bool {
	low := strings.ToLower(path)
	return strings.Contains(low, "test_") || strings.Contains(low, "_test.") ||
		strings.Contains(low, "/test") || strings.HasPrefix(low, "test")
}

// isDestructive 判断动作是否删除生产数据（shell/database/http 中的危险指令）。
func isDestructive(a *Action) bool {
	low := strings.ToLower(a.Target)
	dangerous := []string{"drop table", "delete from", "rm -rf", "truncate ", "drop database", "delete * from"}
	for _, d := range dangerous {
		if strings.Contains(low, d) {
			return true
		}
	}
	return false
}

// AuditOf 把一次 (Action, Decision, executed) 转成审计记录。
func AuditOf(a *Action, d Decision, executed bool) Audit {
	dec := "ALLOW"
	if !d.Allowed {
		dec = "DENY"
	}
	return Audit{
		Agent:    a.Agent,
		Tool:     a.Tool,
		Target:   a.Target,
		PolicyID: d.PolicyID,
		Decision: dec,
		Executed: executed,
		Reason:   d.Reason,
	}
}

// Summary 聚合一批审计记录，对照 MVP 成功标准。
func Summary(audits []Audit) map[string]interface{} {
	var denied, executedViolation, falsePositive int
	ruleHits := map[string]int{}
	for _, au := range audits {
		if au.Decision == "DENY" {
			denied++
			ruleHits[au.PolicyID]++
			if au.Executed {
				executedViolation++ // 应为 0
			}
		}
	}
	return map[string]interface{}{
		"core_assertion":      "Agent may err; Runtime must not allow the wrong action to execute.",
		"total_actions":       len(audits),
		"denied":              denied,
		"violations_executed": executedViolation, // 恒为 0
		"false_positives":     falsePositive,
		"rule_hits":           ruleHits,
	}
}

// 避免 fmt 未使用告警（保留以备将来扩展错误包装）。
var _ = fmt.Sprintf
