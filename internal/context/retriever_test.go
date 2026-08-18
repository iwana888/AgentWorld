package context

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestFakeRetrieverIntentDrivesContent M8.6：FakeRetriever 验证
// "Intent → 不同 ContextBlock" 的机制。WORK 与 HIRE_AGENT 必须返回不同维度。
func TestFakeRetrieverIntentDrivesContent(t *testing.T) {
	r := NewFakeRetriever(nil)

	workReq := &RetrieveRequest{
		AgentID: "alice", Intent: DecisionIntent{Type: "WORK", SkillID: "farming"},
		Limit: 10, BudgetTokens: 500,
	}
	hireReq := &RetrieveRequest{
		AgentID: "alice", Intent: DecisionIntent{Type: "HIRE_AGENT", SkillID: "engineering", TargetAgentID: "bob"},
		RelatedAgentIDs: []string{"bob"}, Limit: 10, BudgetTokens: 500,
	}

	workBlocks, err := r.Retrieve(context.Background(), workReq)
	if err != nil {
		t.Fatal(err)
	}
	hireBlocks, err := r.Retrieve(context.Background(), hireReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(workBlocks) == 0 || len(hireBlocks) == 0 {
		t.Fatal("FakeRetriever 应返回非空块")
	}

	workText := joinBlocks(workBlocks)
	hireText := joinBlocks(hireBlocks)
	if workText == hireText {
		t.Fatal("WORK 与 HIRE_AGENT 的检索结果完全相同——Retrieval-on-demand 未成立")
	}
	// WORK 应包含 farming/工作维度，不应强调雇佣/合同。
	if !strings.Contains(workText, "farming") {
		t.Error("WORK 检索未体现 farming 维度")
	}
	if strings.Contains(workText, "contract_history") {
		t.Error("WORK 检索错误地包含了雇佣合同维度")
	}
	// HIRE_AGENT 应包含雇佣/合同/目标 Agent 历史。
	if !strings.Contains(hireText, "contract_history") && !strings.Contains(hireText, "past_hiring") {
		t.Error("HIRE_AGENT 检索未体现雇佣维度")
	}
	if !strings.Contains(hireText, "Bob_history") {
		t.Error("HIRE_AGENT 检索未体现目标 Agent 历史")
	}
}

// fakeStore 内存版 MemoryStore，用于 M8.7 结构化检索测试。
type fakeStore struct {
	rows []MemoryRow
}

func (s *fakeStore) QueryMemories(ctx context.Context, agentID string, types []string, aboutAgentID string, limit int) ([]MemoryRow, error) {
	typeSet := map[string]bool{}
	for _, tp := range types {
		typeSet[tp] = true
	}
	var out []MemoryRow
	for _, m := range s.rows {
		if m.AgentID != agentID {
			continue
		}
		if !typeSet[m.Type] {
			continue
		}
		// about_agent 记忆若指定了目标 Agent，仅保留关于该 Agent 的。
		if m.Type == "about_agent" && aboutAgentID != "" {
			if !strings.Contains(m.Content, "#"+aboutAgentID) {
				continue
			}
		}
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// TestMemoryRetrieverOnDemand M8.7 核心：给 Agent 100 条记忆
// （5 条 WORK 强相关 / 5 条 HIRE 强相关 / 90 条无关），
// WORK 必须优先拿 WORK 相关且不把 100 条全塞进去；HIRE 同理。
func TestMemoryRetrieverOnDemand(t *testing.T) {
	store := &fakeStore{}
	var rows []MemoryRow
	id := int64(1)
	// 90 条无关（type=other，低 importance）
	for i := 0; i < 90; i++ {
		rows = append(rows, MemoryRow{
			ID: id, AgentID: "alice", Type: "other", Importance: 1,
			Content: "无关琐事记录第 " + itoa(i), CreatedAt: atTime(i),
		})
		id++
	}
	// 5 条 WORK 强相关（type=work，高 importance）
	for i := 0; i < 5; i++ {
		rows = append(rows, MemoryRow{
			ID: id, AgentID: "alice", Type: "work", Importance: 5,
			Content: "WORK 相关工作成功经验 " + itoa(i), CreatedAt: atTime(100 + i),
		})
		id++
	}
	// 5 条 HIRE 强相关（type=hire，高 importance）
	for i := 0; i < 5; i++ {
		rows = append(rows, MemoryRow{
			ID: id, AgentID: "alice", Type: "hire", Importance: 5,
			Content: "HIRE 雇佣历史 " + itoa(i), CreatedAt: atTime(200 + i),
		})
		id++
	}
	store.rows = rows

	r := NewMemoryRetriever(store, nil)

	// WORK 检索：limit=10 → 只应拿到 WORK 相关，绝不拿全部 100 条。
	workReq := &RetrieveRequest{
		AgentID: "alice", Intent: DecisionIntent{Type: "WORK"}, Limit: 10, BudgetTokens: 1000,
	}
	workBlocks, err := r.Retrieve(context.Background(), workReq)
	if err != nil {
		t.Fatal(err)
	}
	// 断言：数量远小于 100（检索是"少加载"），且全部为 WORK 维度。
	if len(workBlocks) >= 100 {
		t.Fatalf("WORK 检索拿了 %d 条，等于全量——Retrieval-on-demand 失败", len(workBlocks))
	}
	if len(workBlocks) == 0 {
		t.Fatal("WORK 检索应至少返回 WORK 相关记忆")
	}
	for _, b := range workBlocks {
		if !strings.Contains(b.Content, "WORK") {
			t.Errorf("WORK 检索混入无关记忆: %s", b.Content)
		}
	}

	// HIRE 检索：同样只拿 HIRE 维度，不拿全量。
	hireReq := &RetrieveRequest{
		AgentID: "alice", Intent: DecisionIntent{Type: "HIRE_AGENT"}, RelatedAgentIDs: []string{}, Limit: 10, BudgetTokens: 1000,
	}
	hireBlocks, err := r.Retrieve(context.Background(), hireReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(hireBlocks) >= 100 {
		t.Fatalf("HIRE 检索拿了 %d 条，等于全量", len(hireBlocks))
	}
	for _, b := range hireBlocks {
		if !strings.Contains(b.Content, "HIRE") {
			t.Errorf("HIRE 检索混入无关记忆: %s", b.Content)
		}
	}

	// WORK 与 HIRE 的检索结果应明显不同。
	if joinBlocks(workBlocks) == joinBlocks(hireBlocks) {
		t.Error("WORK 与 HIRE 检索结果相同，未体现 Intent 驱动")
	}
}

// TestRetrieverIntoCompiler M8.7 集成：Retriever 输出作为 Retrieved 块进入 Compiler，
// 验证 Retrieved 预算分类正确工作。
func TestRetrieverIntoCompiler(t *testing.T) {
	store := &fakeStore{}
	var rows []MemoryRow
	for i := 0; i < 5; i++ {
		rows = append(rows, MemoryRow{
			ID: int64(i + 1), AgentID: "alice", Type: "work", Importance: 5,
			Content: "WORK 经验 " + itoa(i), CreatedAt: atTime(i),
		})
	}
	store.rows = rows

	ret := NewMemoryRetriever(store, nil)
	req := &RetrieveRequest{AgentID: "alice", Intent: DecisionIntent{Type: "WORK"}, Limit: 10, BudgetTokens: 400}

	blocks, err := ret.Retrieve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	// 把检索块作为 DynamicBlocks 里的 Retrieved 类型喂给 Compiler。
	cc, err := NewCompiler(nil).Compile(context.Background(), &ContextRequest{
		AgentID: "alice",
		AgentState: &AgentState{AgentID: "alice", Balance: 120, Goal: "赚币", Intent: "WORK"},
		DecisionIntent: &DecisionIntent{Type: "WORK"},
		StableBlocks: []ContextBlock{
			{ID: "world.rules", Type: TypeWorldRules, Source: "world.rules",
				Content: strings.Repeat("规则 ", 40), Priority: 100, Stable: true},
		},
		DynamicBlocks: blocks, // 检索到的记忆（Type=TypeRetrieved）
	})
	if err != nil {
		t.Fatal(err)
	}
	// 应至少有一个 Retrieved 块进入 DynamicBlocks。
	found := false
	for _, b := range cc.DynamicBlocks {
		if b.Type == TypeRetrieved {
			found = true
		}
	}
	if !found {
		t.Error("Retriever 返回的 Retrieved 块未进入 Compiler 的 DynamicBlocks")
	}
	t.Log("\n" + cc.String())
}

// itoa 简单 int→string（测试用，不依赖 fmt 以提速）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// atTime 由 offset 秒生成时间（测试排序用）。
func atTime(offset int) time.Time {
	return time.Unix(int64(1000000+offset), 0)
}
