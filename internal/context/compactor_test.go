package context

import (
	"context"
	"strings"
	"testing"
)

// big 生成约 n 个 token 的英文文本（RoughEstimator: 4 字符 ≈ 1 token）。
func big(n int) string {
	words := make([]string, 0, n)
	for i := 0; i < n; i++ {
		words = append(words, "word")
	}
	return strings.Join(words, " ")
}

// TestStableNeverCompacted M8.8-1：Stable 永不压缩。
// Stable=1000 tokens, Dynamic=2000 tokens, Budget=1500 → Stable 完整保留，Dynamic 被处理。
func TestStableNeverCompacted(t *testing.T) {
	stable := []ContextBlock{
		{ID: "world.rules", Type: TypeWorldRules, Source: "world.rules", Content: big(1000),
			Priority: PriorityImmutableCore, Stable: true},
	}
	dyn := []ContextBlock{
		{ID: "old.mem", Type: TypeRetrieved, Source: "retrieved.old", Content: big(2000),
			Priority: PriorityLowMemory, Stable: false},
	}
	budget := DefaultBudget()
	budget.MaxTotal = 1500
	budget.System = 1500
	budget.Retrieved = 1500

	c := NewCompiler(nil).WithCompactor(NewFakeCompactor(nil))
	cc, err := c.Compile(context.Background(), &ContextRequest{
		AgentID: "alice", StableBlocks: stable, DynamicBlocks: dyn, Budget: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Stable 块必须原样保留（1000 token 左右，不被压缩）。
	if cc.TokenUsage.Stable < 900 {
		t.Errorf("Stable 被压缩/丢失: got %d tokens", cc.TokenUsage.Stable)
	}
	// 总 token 必须 ≤ MaxTotal（Dynamic 被 Compaction 处理）。
	if cc.TokenUsage.Total > budget.MaxTotal {
		t.Errorf("最终 %d 超过 MaxTotal %d", cc.TokenUsage.Total, budget.MaxTotal)
	}
}

// TestDecisionPriorityOverOldMemory M8.8-2：Decision 优先于 Old Memory。
// Decision(prio 80) vs Old Memory(prio 10)，超预算 → Decision 保留，Old Memory 压缩。
func TestDecisionPriorityOverOldMemory(t *testing.T) {
	oldMem := []ContextBlock{
		{ID: "old1", Type: TypeRetrieved, Source: "retrieved.old1", Content: big(400),
			Priority: PriorityLowMemory, Stable: false},
		{ID: "old2", Type: TypeRetrieved, Source: "retrieved.old2", Content: big(400),
			Priority: PriorityLowMemory, Stable: false},
	}
	budget := DefaultBudget()
	budget.MaxTotal = 500
	budget.Decision = 500
	budget.Retrieved = 10 // 逼 Retrieved 进 Compaction

	c := NewCompiler(nil).WithCompactor(NewFakeCompactor(nil))
	// Decision 由 Compiler 从 CandidateActions 生成，这里用 CandidateActions 注入。
	req := &ContextRequest{
		AgentID: "alice",
		DecisionIntent: &DecisionIntent{Type: "WORK"},
		CandidateActions: []*CandidateAction{
			{ID: "c1", Action: "claim", Label: big(300), Score: 0.9},
		},
		DynamicBlocks: oldMem,
		Budget:        &budget,
	}
	cc, err := c.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Decision 块必须保留（PriorityCurrentDecision 永不压缩）。
	foundDec := false
	for _, b := range cc.DecisionBlocks {
		if b.Type == TypeDecision {
			foundDec = true
		}
	}
	if !foundDec {
		t.Error("Decision Context 被压缩/丢弃，违反 Policy")
	}
	// 总 token 必须 ≤ MaxTotal。
	if cc.TokenUsage.Total > budget.MaxTotal {
		t.Errorf("最终 %d 超过 MaxTotal %d", cc.TokenUsage.Total, budget.MaxTotal)
	}
}

// TestRecentEventPreferredOverOld M8.8-3：Recent Event 优先于 Old Event。
// EventA（2 天前，prio 低）vs EventB（10 秒前，prio 高），预算不足 → 保留 B 处理 A。
func TestRecentEventPreferredOverOld(t *testing.T) {
	// 用 Priority 表达新旧：Recent=PriorityRecentEvent, Old=PriorityOldEvent。
	oldEvent := ContextBlock{ID: "evt.old", Type: TypeEvent, Source: "event.old",
		Content: big(400), Priority: PriorityOldEvent, Stable: false}
	recentEvent := ContextBlock{ID: "evt.recent", Type: TypeEvent, Source: "event.recent",
		Content: big(400), Priority: PriorityRecentEvent, Stable: false}
	budget := DefaultBudget()
	budget.MaxTotal = 500
	budget.WorldEvent = 500
	budget.State = 0

	c := NewCompiler(nil).WithCompactor(NewFakeCompactor(nil))
	cc, err := c.Compile(context.Background(), &ContextRequest{
		AgentID: "alice",
		DynamicBlocks: []ContextBlock{oldEvent, recentEvent},
		Budget:        &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Recent Event 必须保留。
	foundRecent := false
	for _, b := range cc.DynamicBlocks {
		if b.ID == "evt.recent" {
			foundRecent = true
		}
	}
	if !foundRecent {
		t.Error("Recent Event 被丢弃，违反 Policy（应优先保留）")
	}
	if cc.TokenUsage.Total > budget.MaxTotal {
		t.Errorf("最终 %d 超过 MaxTotal %d", cc.TokenUsage.Total, budget.MaxTotal)
	}
}

// TestRetrievedMemoryCompacted M8.8-4：Retriever→Retrieved→超限→Compaction 链路通。
func TestRetrievedMemoryCompacted(t *testing.T) {
	store := &fakeStore{}
	for i := 0; i < 5; i++ {
		store.rows = append(store.rows, MemoryRow{
			ID: int64(i + 1), AgentID: "alice", Type: "work", Importance: 5,
			Content: "WORK farming memory " + big(300), CreatedAt: atTime(i),
		})
	}
	ret := NewMemoryRetriever(store, nil)
	budget := DefaultBudget()
	budget.MaxTotal = 800
	budget.Retrieved = 800 // 检索会拉满
	// 但故意让整体超：再加一条大 State 与大 Decision，逼 Compaction 处理 Retrieved。

	req := &ContextRequest{
		AgentID: "alice",
		AgentState: &AgentState{AgentID: "alice", Balance: 120},
		DecisionIntent: &DecisionIntent{Type: "WORK"},
		CandidateActions: []*CandidateAction{
			{ID: "c1", Action: "claim", Label: big(80), Score: 0.9}, // Decision ~105
		},
		StableBlocks: []ContextBlock{
			{ID: "world.rules", Type: TypeWorldRules, Source: "world.rules",
				Content: big(80), Priority: PriorityImmutableCore, Stable: true},
		},
		DynamicBlocks: []ContextBlock{
			{ID: "agent.state", Type: TypeAgentState, Source: "agent.state",
				Content: big(80), Priority: PriorityCurrentState, Stable: false},
		},
		Retriever: ret,
		Budget:    &budget,
	}
	c := NewCompiler(nil).WithCompactor(NewFakeCompactor(nil))
	cc, err := c.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Retrieved 块应存在（经 Retriever 拉取），且最终 ≤ MaxTotal（被 Compaction 压过）。
	foundRetrieved := false
	for _, b := range cc.DynamicBlocks {
		if b.Type == TypeRetrieved {
			foundRetrieved = true
		}
	}
	if !foundRetrieved {
		t.Error("Retriever 拉取的 Retrieved 块未进入 Context")
	}
	if cc.TokenUsage.Total > budget.MaxTotal {
		t.Errorf("最终 %d 超过 MaxTotal %d（Compaction 未生效）", cc.TokenUsage.Total, budget.MaxTotal)
	}
	t.Log("\n" + cc.String())
}

// TestFinalContextWithinBudget M8.8-5：硬约束——最终 ≤ MaxTotal，否则明确标记。
func TestFinalContextWithinBudget(t *testing.T) {
	// 构造必然超限场景：Stable 占满，Dynamic 也大，但 Stable 不可压缩。
	stable := []ContextBlock{
		{ID: "world.rules", Type: TypeWorldRules, Source: "world.rules", Content: big(1000),
			Priority: PriorityImmutableCore, Stable: true},
	}
	dyn := []ContextBlock{
		{ID: "old1", Type: TypeRetrieved, Source: "retrieved.old1", Content: big(800),
			Priority: PriorityLowMemory, Stable: false},
	}
	budget := DefaultBudget()
	budget.MaxTotal = 1500
	budget.System = 1500
	budget.Retrieved = 1500

	c := NewCompiler(nil).WithCompactor(NewFakeCompactor(nil))
	cc, err := c.Compile(context.Background(), &ContextRequest{
		AgentID: "alice", StableBlocks: stable, DynamicBlocks: dyn, Budget: &budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 硬约束：要么 ≤ MaxTotal，要么明确 OverBudget（不能悄悄超）。
	if cc.TokenUsage.Total > budget.MaxTotal && !cc.TokenUsage.OverBudget {
		t.Errorf("超预算但未标记 OverBudget：%d > %d", cc.TokenUsage.Total, budget.MaxTotal)
	}
	// 至少 Stable 必须保留（不可压缩）。
	if cc.TokenUsage.Stable < 900 {
		t.Errorf("Stable 不应被压缩，但只剩 %d", cc.TokenUsage.Stable)
	}
}
