package context

import (
	"context"
	"testing"
)

// mkCompiledWithTokens 直接构造带精确 Token 分项的 CompiledContext，
// 绕过 Compiler，便于断言 Accounting 的纯读取/统计逻辑。
func mkCompiledWithTokens(stable, state, retrieved, event, decision, compacted int) *CompiledContext {
	ctxTok := stable + state + retrieved + event + decision
	return &CompiledContext{
		TokenUsage: TokenUsage{
			StableTokens:    stable,
			StateTokens:     state,
			RetrievedTokens: retrieved,
			EventTokens:     event,
			DecisionTokens:  decision,
			CompactedTokens: compacted,
			ContextTokens:   ctxTok,
			Stable:          stable,
			Dynamic:         state + retrieved + event,
			Decision:        decision,
			Retrieved:       retrieved,
			Compacted:       compacted,
			Total:           ctxTok,
		},
	}
}

// TestAccountingCorrectTotals 验证 Runtime 成本分项正确汇总。
//   Stable=500 State=100 Event=200 Decision=300 → ContextTokens=1100
func TestAccountingCorrectTotals(t *testing.T) {
	cc := mkCompiledWithTokens(500, 100, 0, 200, 300, 0)
	a := AccountCompile(cc, "Alice", "t1")
	if a.StableTokens != 500 {
		t.Errorf("StableTokens=%d want 500", a.StableTokens)
	}
	if a.StateTokens != 100 {
		t.Errorf("StateTokens=%d want 100", a.StateTokens)
	}
	if a.EventTokens != 200 {
		t.Errorf("EventTokens=%d want 200", a.EventTokens)
	}
	if a.DecisionTokens != 300 {
		t.Errorf("DecisionTokens=%d want 300", a.DecisionTokens)
	}
	if a.ContextTokens != 1100 {
		t.Errorf("ContextTokens=%d want 1100", a.ContextTokens)
	}
	// AccountCompile 不修改原对象。
	if cc.TokenUsage.ContextTokens != 1100 {
		t.Errorf("AccountCompile mutated source ContextTokens=%d", cc.TokenUsage.ContextTokens)
	}
}

// TestAccountingRetrieved 验证 Retriever 加多少，RetrievedTokens 就是多少。
func TestAccountingRetrieved(t *testing.T) {
	for _, retr := range []int{0, 130, 430, 1000} {
		cc := mkCompiledWithTokens(600, 100, retr, 200, 300, 0)
		a := AccountCompile(cc, "Bob", "t2")
		if a.RetrievedTokens != retr {
			t.Errorf("RetrievedTokens=%d want %d", a.RetrievedTokens, retr)
		}
	}
}

// TestAccountingCompaction 验证 Compaction 正确统计。
//   Before=2000 After=800 → CompactedTokens=1200
func TestAccountingCompaction(t *testing.T) {
	const before, after = 2000, 800
	// 用 RetrievedTokens 字段模拟"压缩前的 Retrieved 量=before，压缩后=after"。
	// 这里直接验证 CompactedTokens 字段的语义：由 Compiler 在 compactAndRecompile
	// 中回填（before-after）。我们构造带该字段的 CompiledContext 断言读取正确。
	cc := mkCompiledWithTokens(600, 100, after, 200, 300, before-after)
	a := AccountCompile(cc, "Carol", "t3")
	if a.CompactedTokens != before-after {
		t.Errorf("CompactedTokens=%d want %d", a.CompactedTokens, before-after)
	}
	if a.RetrievedTokens != after {
		t.Errorf("压缩后 RetrievedTokens=%d want %d", a.RetrievedTokens, after)
	}
	// 端到端：用真实 Compiler+Compactor 验证 CompactedTokens 由 Runtime 正确回填。
	comp := NewCompiler(roughEstimate).WithCompactor(NewFakeCompactor(roughEstimate))
	big := func(n int) string { return string(make([]byte, n)) }
	var dyn []ContextBlock
	for i := 0; i < 50; i++ {
		dyn = append(dyn, ContextBlock{
			ID: "evt" + string(rune('a'+i%26)) + itoa(i), Type: TypeEvent,
			Source: "evt", Content: big(200), Stable: false, Priority: PriorityOldEvent,
		})
	}
	req := &ContextRequest{
		AgentID:  "Dave",
		Budget:   &ContextBudget{MaxTotal: 100, System: 1000, State: 0, Retrieved: 200, WorldEvent: 100, Decision: 0, Reserved: 0},
		StableBlocks: []ContextBlock{{ID: "s", Type: TypeWorldRules, Content: big(100), Stable: true, Priority: PriorityImmutableCore}},
		DynamicBlocks: dyn,
	}
	cc2, err := comp.Compile(context.Background(), req)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if cc2.TokenUsage.CompactedTokens <= 0 {
		t.Errorf("expected positive CompactedTokens after compaction, got %d (usage=%+v)", cc2.TokenUsage.CompactedTokens, cc2.TokenUsage)
	}
}

// TestAccountingZeroSideEffect 验证：同一 Agent 两次决策输入完全一致，
// Accounting 开关前后 Compile 结果一致（Accounting 不影响决策/Planner/Economy）。
func TestAccountingZeroSideEffect(t *testing.T) {
	comp := NewCompiler(roughEstimate).WithCompactor(NewFakeCompactor(roughEstimate))
	buildReq := func() *ContextRequest {
		budget := DefaultBudget()
		return &ContextRequest{
			AgentID: "Alice",
			DecisionIntent: &DecisionIntent{Type: "WORK"},
			CandidateActions: []*CandidateAction{
				{Action: "work", Label: "Work", Score: 0.9},
				{Action: "wait", Label: "Wait", Score: 0.3},
			},
			Budget: &budget,
			StableBlocks: []ContextBlock{
				{ID: "world.rules", Type: TypeWorldRules, Source: "world.rules", Content: "rule: no steal", Stable: true, Priority: PriorityImmutableCore},
				{ID: "agent.id", Type: TypeAgentIdentity, Source: "agent.id", Content: "Alice", Stable: true, Priority: PriorityImmutableCore},
			},
			DynamicBlocks: []ContextBlock{
				{ID: "state", Type: TypeAgentState, Source: "state", Content: "balance=120", Stable: false, Priority: PriorityCurrentState},
			},
		}
	}

	// 第一次：不开 Accounting。
	cc1, err := comp.Compile(context.Background(), buildReq())
	if err != nil {
		t.Fatalf("compile1: %v", err)
	}
	// 第二次：开 Accounting（生成账本条目），输入完全一致。
	ledger := NewTokenLedger()
	cc2, err := comp.Compile(context.Background(), buildReq())
	if err != nil {
		t.Fatalf("compile2: %v", err)
	}
	ledger.Append(AccountCompile(cc2, "Alice", "t1"))
	ledger.Append(AccountCompile(cc2, "Alice", "t2"))

	// 决策结果（块集合与 TokenUsage）必须一致——Accounting 未改变 Runtime 行为。
	if cc1.TokenUsage.ContextTokens != cc2.TokenUsage.ContextTokens {
		t.Errorf("ContextTokens changed by accounting: %d vs %d", cc1.TokenUsage.ContextTokens, cc2.TokenUsage.ContextTokens)
	}
	if len(cc1.StableBlocks) != len(cc2.StableBlocks) || len(cc1.DynamicBlocks) != len(cc2.DynamicBlocks) {
		t.Errorf("block counts changed by accounting")
	}
	if cc1.TokenUsage.Total != cc2.TokenUsage.Total {
		t.Errorf("Total changed by accounting: %d vs %d", cc1.TokenUsage.Total, cc2.TokenUsage.Total)
	}
	// Ledger 仅持有拷贝，不反向影响 cc2。
	if cc2.TokenUsage.ContextTokens != ledger.entries[0].ContextTokens {
		t.Errorf("ledger entry diverged from source")
	}
}
