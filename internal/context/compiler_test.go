package context

import (
	"context"
	"strings"
	"testing"
)

// makeReq 构造一个最小请求，便于各测试复用。
func makeReq(intent *DecisionIntent, cands []*CandidateAction) *ContextRequest {
	return &ContextRequest{
		AgentID: "alice",
		AgentState: &AgentState{
			AgentID: "alice", Balance: 120, Goal: "赚到 1000 coins", Intent: "WORK",
			Extra: map[string]string{"wealth_rank": "3/10"},
		},
		DecisionIntent: intent,
		CandidateActions: cands,
		StableBlocks: []ContextBlock{
			{ID: "world.rules", Type: TypeWorldRules, Source: "world.rules",
				Content: strings.Repeat("世界规则 ", 60), Priority: 100, Stable: true},
			{ID: "agent.identity", Type: TypeAgentIdentity, Source: "agent.identity",
				Content: "名字: Alice 职业: Engineer", Priority: 95, Stable: true},
			{ID: "agent.personality", Type: TypePersonality, Source: "agent.personality",
				Content: "性格: 冒险、独立", Priority: 90, Stable: true},
			{ID: "tool.schema", Type: TypeToolSchema, Source: "tool.schema",
				Content: "可用工具: repair_machine query_machine", Priority: 80, Stable: true},
		},
		DynamicBlocks: []ContextBlock{
			{ID: "agent.state", Type: TypeAgentState, Source: "agent.state",
				Content: "余额: 120 coins 财富排名: 3/10", Priority: 70, Stable: false},
			{ID: "recent.events", Type: TypeEvent, Source: "recent.events",
				Content: "市场热议: AI 编程工具刷屏", Priority: 50, Stable: false},
		},
	}
}

// 测试 1：Stable Block 是否保持稳定（进入 StableBlocks）。
func TestStableBlocksPreserved(t *testing.T) {
	c := NewCompiler(nil)
	cc, err := c.Compile(context.Background(), makeReq(&DecisionIntent{Type: "WORK"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(cc.StableBlocks) == 0 {
		t.Fatal("期望有 Stable Blocks，实际为空")
	}
	// 所有 Stable 块的 Type 都应属于 Immutable/Semi-Stable 语义（Stable=true）。
	for _, b := range cc.StableBlocks {
		if !b.Stable {
			t.Errorf("StableBlocks 中出现非稳定块: %s", b.Source)
		}
	}
	// 特定块要存在
	want := map[string]bool{"world.rules": false, "agent.identity": false, "agent.personality": false}
	for _, b := range cc.StableBlocks {
		if _, ok := want[b.ID]; ok {
			want[b.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("Stable 块缺失: %s", id)
		}
	}
}

// 测试 2：Dynamic Block 是否正确进入 Context。
func TestDynamicBlocksIncluded(t *testing.T) {
	c := NewCompiler(nil)
	cc, err := c.Compile(context.Background(), makeReq(&DecisionIntent{Type: "WORK"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	foundState, foundEvent := false, false
	for _, b := range cc.DynamicBlocks {
		switch b.Type {
		case TypeAgentState:
			foundState = true
		case TypeEvent:
			foundEvent = true
		}
	}
	if !foundState {
		t.Error("未找到 AgentState 动态块")
	}
	if !foundEvent {
		t.Error("未找到 Event 动态块")
	}
}

// 测试 3：Budget 超限是否正确处理（不 panic，且标记 OverBudget）。
func TestBudgetOverflowHandled(t *testing.T) {
	// 把 System 预算压到极小，逼 Stable 截断。
	tiny := DefaultBudget()
	tiny.MaxTotal = 200
	tiny.System = 50
	tiny.State = 20
	tiny.WorldEvent = 20
	tiny.Decision = 20
	req := makeReq(&DecisionIntent{Type: "WORK"}, nil)
	req.Budget = &tiny
	c := NewCompiler(nil)
	cc, err := c.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if cc.TokenUsage.Total > tiny.MaxTotal {
		if !cc.TokenUsage.OverBudget {
			t.Error("超出 MaxTotal 但未标记 OverBudget")
		}
	}
	// M8.8 语义：Stable 是不可压缩的 Immutable Core，宁可越过分类小预算也要保留，
	// 由 MaxTotal 硬约束在末尾统一兜底。因此不再保证 Stable <= System，
	// 而是保证 Stable 块"被保留"（不被 Budget 截断丢弃）。
	foundRules := false
	for _, b := range cc.StableBlocks {
		if b.ID == "world.rules" {
			foundRules = true
		}
	}
	if !foundRules {
		t.Error("Stable 块被 Budget 截断丢弃——违反 M8.8 不可压缩策略")
	}

	// 注入 Compactor 后，可压缩的 Dynamic 块应被压掉；但 Stable 是不可压缩的
	// Immutable Core，若它本身就超过 MaxTotal，Compaction 也无能为力——此时应
	// 明确标记 OverBudget（硬约束：要么 ≤ MaxTotal，要么明确状态，绝不悄悄超）。
	cc2, err := NewCompiler(nil).WithCompactor(NewFakeCompactor(nil)).Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if cc2.TokenUsage.Total > tiny.MaxTotal && !cc2.TokenUsage.OverBudget {
		t.Errorf("注入 Compactor 后 %d > MaxTotal %d 但未标记 OverBudget", cc2.TokenUsage.Total, tiny.MaxTotal)
	}
}

// 测试 4：Context 总 Token 是否正确计算。
func TestTotalTokensComputed(t *testing.T) {
	c := NewCompiler(nil)
	cc, err := c.Compile(context.Background(), makeReq(&DecisionIntent{Type: "WORK"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	want := cc.TokenUsage.Stable + cc.TokenUsage.Dynamic + cc.TokenUsage.Decision
	if cc.TokenUsage.Total != want {
		t.Errorf("Total=%d 但分项之和=%d", cc.TokenUsage.Total, want)
	}
	if cc.TokenUsage.Total <= 0 {
		t.Error("Total token 不应为 0")
	}
}

// 测试 5（最关键）：不同 Intent 应产生明显不同的 Decision Context。
// 若 WORK 与 HIRE_AGENT 的 Decision 块几乎一样 → 说明 Context Runtime
// 还只是 Prompt Builder；差异明显 → 架构开始成立。
func TestDifferentIntentDifferentDecision(t *testing.T) {
	c := NewCompiler(nil)

	workReq := makeReq(&DecisionIntent{Type: "WORK"}, []*CandidateAction{
		{ID: "claim:job1", Action: "claim", Label: "接工作: 修机器", Cost: 0, Reward: 80, Score: 0.9},
		{ID: "wait", Action: "wait", Label: "观望", Score: 0.2},
	})
	hireReq := makeReq(&DecisionIntent{Type: "HIRE_AGENT", SkillID: "engineering"},
		[]*CandidateAction{
			{ID: "hire:42", Action: "hire_agent", Label: "雇 Bob 做工程", Cost: 50, Reward: 120, Score: 0.85,
				Detail: "成功率 90% 声誉 5"},
			{ID: "buy_skill:engineer", Action: "buy_skill", Label: "买 Engineer 技能", Cost: 100, Score: 0.6},
		})

	workCC, err := c.Compile(context.Background(), workReq)
	if err != nil {
		t.Fatal(err)
	}
	hireCC, err := c.Compile(context.Background(), hireReq)
	if err != nil {
		t.Fatal(err)
	}

	workDecision := joinBlocks(workCC.DecisionBlocks)
	hireDecision := joinBlocks(hireCC.DecisionBlocks)

	if workDecision == hireDecision {
		t.Fatal("WORK 与 HIRE_AGENT 的 Decision Context 完全相同——Context Runtime 退化成 Prompt Builder")
	}
	// HIRE_AGENT 的 Decision 应体现 skill=engineering 与 hire 候选。
	if !strings.Contains(hireDecision, "engineering") {
		t.Error("HIRE_AGENT 的 Decision 未体现 Skill 维度")
	}
	if !strings.Contains(hireDecision, "雇") && !strings.Contains(hireDecision, "hire") {
		t.Error("HIRE_AGENT 的 Decision 未体现雇佣候选")
	}
	// WORK 的 Decision 不应明显包含 hire 维度。
	if strings.Contains(workDecision, "hire_agent") && strings.Contains(workDecision, "engineering") {
		t.Error("WORK 的 Decision 错误地包含了 HIRE 维度")
	}
}

// joinBlocks 把多个块的 Content 拼起来比较。
func joinBlocks(blocks []ContextBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		sb.WriteString(b.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestStringRendering 验证 Observatory 快照渲染可读。
func TestStringRendering(t *testing.T) {
	c := NewCompiler(nil)
	cc, err := c.Compile(context.Background(), makeReq(&DecisionIntent{Type: "HIRE_AGENT", SkillID: "engineering"},
		[]*CandidateAction{{ID: "hire:42", Action: "hire_agent", Label: "雇 Bob", Cost: 50, Reward: 120}}))
	if err != nil {
		t.Fatal(err)
	}
	out := cc.String()
	if !strings.Contains(out, "CompiledContext") {
		t.Error("快照缺少标题")
	}
	if !strings.Contains(out, "DECISION") {
		t.Error("快照缺少 DECISION 分组")
	}
	if !strings.Contains(out, "Total:") {
		t.Error("快照缺少 Total")
	}
	t.Log("\n" + out)
}
