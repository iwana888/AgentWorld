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
	"path/filepath"
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

// DecisionType 是 MVP 2 引入的四种决策之一。
//
//	ALLOW  → 直接执行
//	DENY   → 禁止执行（Runtime 阻止，永不落地）
//	ASK    → 请求人工批准；HumanApproval=true 后等价于 ALLOW
//	MODIFY → Runtime 返回一个“建议修改后的动作”，由调用方决定是否采用（第一版绝不偷偷替 Agent 改动作）
const (
	DecisionAllow  DecisionType = "ALLOW"
	DecisionDeny   DecisionType = "DENY"
	DecisionAsk    DecisionType = "ASK"
	DecisionModify DecisionType = "MODIFY"
)

// DecisionType 是决策类型枚举。
type DecisionType string

// Decision 是 Guard 对一次 Action 的结论（MVP 2: 4 态）。
type Decision struct {
	Type          DecisionType `json:"type"`
	Allowed       bool         `json:"allowed"`       // 兼容字段：ALLOW/MODIFY/(ASK且已批准) 为 true
	PolicyID      string       `json:"policy_id,omitempty"`
	Reason        string       `json:"reason,omitempty"`
	Severity      Severity     `json:"severity,omitempty"`
	Suggested     *Action      `json:"suggested,omitempty"` // MODIFY 专用：Runtime 建议改成的动作
	HumanApproval bool         `json:"human_approval,omitempty"` // ASK 专用：人工已批准
}

// Guard 是统一拦截接口 —— Agent 的“安全边界”。
type Guard interface {
	Check(ctx context.Context, action *Action) Decision
}

// Audit 是一条审计记录：Agent 想干什么 → 触发什么规则 → 决策 → 是否执行 → 为什么。
// 这是 Reliability Runtime 最具商业价值的部分：每一次边界决策都可追溯。
type Audit struct {
	Agent    string `json:"agent"`
	Tool     string `json:"tool"`
	Target   string `json:"target"`
	PolicyID string `json:"policy_id,omitempty"`
	Type     string `json:"decision"` // ALLOW / DENY / ASK / MODIFY
	Executed bool   `json:"executed"` // DENY 时恒为 false（Runtime 不允许执行）
	Reason   string `json:"reason,omitempty"`
}

// Allowed 是放行的快捷决策（同时保持 Allowed=true 兼容旧调用方）。
func Allowed() Decision {
	return Decision{Type: DecisionAllow, Allowed: true}
}

// Denied 构造一个拒绝决策。
func Denied(policyID, reason string, sev Severity) Decision {
	return Decision{Type: DecisionDeny, Allowed: false, PolicyID: policyID, Reason: reason, Severity: sev}
}

// AskForApproval 构造一个需要人工批准的决策。
func AskForApproval(policyID, reason string, sev Severity) Decision {
	return Decision{Type: DecisionAsk, Allowed: false, PolicyID: policyID, Reason: reason, Severity: sev}
}

// ModifyTo 构造一个“建议修改后执行”的决策。Runtime 只给出建议动作，绝不偷偷替 Agent 改。
func ModifyTo(policyID, reason string, sev Severity, suggested *Action) Decision {
	return Decision{Type: DecisionModify, Allowed: true, PolicyID: policyID, Reason: reason, Severity: sev, Suggested: suggested}
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

// DefaultPolicies 返回 MVP 2 内置的六类规则（覆盖 4 态决策）。
func DefaultPolicies() []Policy {
	return []Policy{
		{ID: "TEST_FILE_IMMUTABLE", Name: "Test files are immutable",
			Description: "Agents must not modify test_*.pas / *_test.go etc. The write is redirected to the source unit (MODIFY).", Severity: SeverityHigh, Enabled: true},
		{ID: "NO_FORCE_PUSH", Name: "No force push",
			Description: "git push --force is forbidden (history rewriting).", Severity: SeverityCritical, Enabled: true},
		{ID: "REQUIRE_TEST", Name: "Require test after code change",
			Description: "After modifying production code, a test run is required before submit.", Severity: SeverityMedium, Enabled: true},
		{ID: "PRODUCTION_DELETE", Name: "No production data deletion",
			Description: "Deleting production data (drop/delete/rm on prod) is forbidden.", Severity: SeverityCritical, Enabled: true},
		{ID: "DEPLOY_PROD_ASK", Name: "Ask before production deploy",
			Description: "Deploying to production requires human approval (ASK).", Severity: SeverityHigh, Enabled: true},
		{ID: "NO_MASS_SPEND", Name: "No unchecked large spend",
			Description: "Large spend/transfer above threshold requires review (DENY by default).", Severity: SeverityHigh, Enabled: true},
		{ID: "RESTRICTED_KEY_ASK", Name: "Ask before issuing privileged keys",
			Description: "Issuing master/admin keys requires human approval (ASK).", Severity: SeverityHigh, Enabled: true},
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

// match 判断单个 Policy 是否命中该 Action，命中则按该规则返回 4 态之一。
func (g *ToolGuard) match(p Policy, a *Action) (Decision, bool) {
	switch p.ID {
	case "TEST_FILE_IMMUTABLE":
		// MODIFY：不偷偷改动作，只返回“建议重定向到源码单元”的建议动作。
		if a.Tool == "write_file" && isTestFile(a.Target) {
			src := testToSource(a.Target)
			suggested := &Action{Agent: a.Agent, Tool: "write_file", Target: src, Args: a.Args, Modified: true}
			return ModifyTo(p.ID,
				"Test files are immutable; redirect the change to the source unit "+src+".",
				p.Severity, suggested), true
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
	case "DEPLOY_PROD_ASK":
		if a.Tool == "deploy" && isProductionTarget(a.Target) {
			return AskForApproval(p.ID, "Production deploy requires human approval.", p.Severity), true
		}
	case "NO_MASS_SPEND":
		if a.Tool == "spend" || a.Tool == "transfer" {
			if amt, ok := a.Args["amount"]; ok && parseAmount(amt) >= 1000 {
				return Denied(p.ID, "Large spend/transfer (>=1000) is not auto-approved.", p.Severity), true
			}
		}
	case "RESTRICTED_KEY_ASK":
		if a.Tool == "issue_key" && isPrivilegedKey(a.Target) {
			return AskForApproval(p.ID, "Issuing a master/admin key requires human approval.", p.Severity), true
		}
	case "REQUIRE_TEST":
		// Outcome 级：修改了生产代码却未执行测试就 submit → DENY。
		if a.Tool == "submit" && a.Modified {
			return Denied(p.ID, "Code was modified; run tests before submit.", p.Severity), true
		}
	}
	return Decision{}, false
}

// testToSource 把测试文件路径映射到对应的源码单元路径（MODIFY 建议目标）。
// 例如 tests/test_calc.pas → src/calc.pas。重定向目标固定落在 src/ 下，
// 因为测试文件的改动本质属于源码单元的修复。
func testToSource(testPath string) string {
	base := filepath.Base(testPath)
	// 去掉 test_ 前缀与 _test 后缀
	name := strings.TrimSuffix(base, filepath.Ext(base))
	name = strings.TrimPrefix(name, "test_")
	name = strings.TrimSuffix(name, "_test")
	if name == "" {
		name = "module"
	}
	return filepath.Join("src", name+filepath.Ext(base))
}

// isProductionTarget 判断 deploy 目标是否为生产环境。
func isProductionTarget(target string) bool {
	low := strings.ToLower(target)
	return strings.Contains(low, "prod") || strings.Contains(low, "production") || strings.Contains(low, "live")
}

// parseAmount 从字符串中解析数值金额（取首个数字序列）。
func parseAmount(s string) int {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		} else if len(digits) > 0 {
			break // 遇到非数字即停止（取前缀数值）
		}
	}
	if len(digits) == 0 {
		return 0
	}
	n := 0
	for _, r := range digits {
		n = n*10 + int(r-'0')
	}
	return n
}

// isPrivilegedKey 判断 key 是否为特权密钥（master/admin/root）。
func isPrivilegedKey(target string) bool {
	low := strings.ToLower(target)
	return strings.Contains(low, "master") || strings.Contains(low, "admin") || strings.Contains(low, "root")
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
	return Audit{
		Agent:    a.Agent,
		Tool:     a.Tool,
		Target:   a.Target,
		PolicyID: d.PolicyID,
		Type:     string(d.Type),
		Executed: executed,
		Reason:   d.Reason,
	}
}

// Summary 聚合一批审计记录，对照 MVP 2 成功标准（4 态）。
func Summary(audits []Audit) map[string]interface{} {
	var allow, deny, ask, modify, executedViolation int
	byType := map[string]int{}
	ruleHits := map[string]int{}
	for _, au := range audits {
		byType[au.Type]++
		ruleHits[au.PolicyID]++
		switch au.Type {
		case "ALLOW":
			allow++
		case "DENY":
			deny++
			if au.Executed {
				executedViolation++ // 应为 0
			}
		case "ASK":
			ask++
		case "MODIFY":
			modify++
		}
	}
	return map[string]interface{}{
		"core_assertion":      "Agent may err; Runtime must not allow the wrong action to execute.",
		"total_actions":       len(audits),
		"allow":               allow,
		"deny":                deny,
		"ask":                 ask,
		"modify":              modify,
		"violations_executed": executedViolation, // 恒为 0
		"by_type":             byType,
		"rule_hits":           ruleHits,
	}
}

// CrossWorldCase 是跨 World Demo 的一个样例动作。
type CrossWorldCase struct {
	World string
	Act   Action
}

// CrossWorldCases 返回一组跨 World 演示样例，证明同一 Guard 与 World 无关。
func CrossWorldCases() []CrossWorldCase {
	return []CrossWorldCase{
		{World: "Pascal", Act: Action{Tool: "write_file", Target: "tests/test_calc.pas"}},
		{World: "Economy", Act: Action{Tool: "spend", Target: "1000 coins", Args: map[string]string{"amount": "1000"}}},
		{World: "Hotel", Act: Action{Tool: "issue_key", Target: "master-key-001"}},
		{World: "Shell", Act: Action{Tool: "shell", Target: "rm -rf /"}},
		{World: "Git", Act: Action{Tool: "git", Target: "push --force origin main"}},
	}
}

// RunCrossWorldDemo 用同一套默认 Guard 跑跨 World 样例，返回每个 World 的决策。
// 它不依赖任何具体 World 的实现——这就是“Reliability Runtime 与 World 无关”的证据。
func RunCrossWorldDemo() map[string]interface{} {
	g := NewToolGuard()
	ctx := context.Background()
	results := make([]map[string]interface{}, 0, len(CrossWorldCases()))
	audits := make([]Audit, 0, len(CrossWorldCases()))
	for _, c := range CrossWorldCases() {
		d := g.Check(ctx, &c.Act)
		executed := false
		if d.Type == DecisionAllow || (d.Type == DecisionAsk && d.HumanApproval) || d.Type == DecisionModify {
			// 仅 ALLOW / 已批准 ASK / 采用建议的 MODIFY 会执行；DENY 永不执行
			executed = d.Type == DecisionAllow || (d.Type == DecisionAsk && d.HumanApproval)
		}
		audits = append(audits, AuditOf(&c.Act, d, executed))
		res := map[string]interface{}{
			"world":    c.World,
			"tool":     c.Act.Tool,
			"target":   c.Act.Target,
			"decision": string(d.Type),
			"policy":   d.PolicyID,
			"reason":   d.Reason,
			"executed": executed,
		}
		if d.Type == DecisionModify && d.Suggested != nil {
			res["suggested_target"] = d.Suggested.Target
		}
		results = append(results, res)
	}
	return map[string]interface{}{
		"experiment": "Reliability Runtime — Cross-World Policy Decision (MVP 2)",
		"assertion":  "The same Runtime routes actions from ANY world; worlds only supply Actions.",
		"results":    results,
		"summary":    Summary(audits),
	}
}
