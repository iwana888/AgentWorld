package context

import (
	"context"
	"strings"
	"testing"
)

// mkBlock 构造一个已估算 Token 的测试块。
func mkBlock(id string, t BlockType, content string, stable bool, prio int) ContextBlock {
	return ContextBlock{
		ID: id, Type: t, Source: id, Content: content,
		Stable: stable, Priority: prio, Tokens: roughEstimate(content),
	}
}

// sampleCompiled 构造一个含 STABLE/STATE/RETRIEVED/EVENT/DECISION 的 CompiledContext。
func sampleCompiled() *CompiledContext {
	return &CompiledContext{
		StableBlocks: []ContextBlock{
			mkBlock("world.rules", TypeWorldRules, "WORLD RULES: no steal", true, PriorityImmutableCore),
			mkBlock("agent.identity", TypeAgentIdentity, "IDENTITY: Alice the trader", true, PriorityImmutableCore),
			mkBlock("agent.personality", TypePersonality, "PERSONALITY: cautious", true, PriorityImmutableCore),
		},
		DynamicBlocks: []ContextBlock{
			mkBlock("agent.state", TypeAgentState, "STATE: balance=120", false, PriorityCurrentState),
			mkBlock("memory.1", TypeRetrieved, "RETRIEVED: met Bob yesterday", false, PriorityRetrieved),
			mkBlock("event.1", TypeEvent, "EVENT: market crash", false, PriorityRecentEvent),
		},
		DecisionBlocks: []ContextBlock{
			mkBlock("decision.intent", TypeDecision, "DECISION: WORK or HIRE", false, PriorityCurrentDecision),
		},
		TokenUsage: TokenUsage{Total: 100, ContextTokens: 100},
	}
}

// TestAdapterStableToSystem 验证：STABLE 永远进 system 前缀，且只有 STABLE 进 system。
func TestAdapterStableToSystem(t *testing.T) {
	cc := sampleCompiled()
	ad := NewOpenAICompatibleAdapter()
	msgs, err := ad.CompileMessages(context.Background(), cc)
	if err != nil {
		t.Fatalf("CompileMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system+user), got %d", len(msgs))
	}
	sys, user := msgs[0], msgs[1]
	if sys.Role != "system" {
		t.Fatalf("first message role = %q, want system", sys.Role)
	}
	if user.Role != "user" {
		t.Fatalf("second message role = %q, want user", user.Role)
	}
	// STABLE 三块内容必须全在 system 中，且不应泄漏到 user。
	for _, want := range []string{"WORLD RULES", "IDENTITY", "PERSONALITY"} {
		if !strings.Contains(sys.Content, want) {
			t.Errorf("system missing stable content %q\nsystem=%q", want, sys.Content)
		}
		if strings.Contains(user.Content, want) {
			t.Errorf("stable content %q leaked into user message\nuser=%q", want, user.Content)
		}
	}
}

// TestAdapterDynamicMapping 验证：State/Retrieved/Event/Decision 正确映射到 user，
// 且顺序固定 STATE → RETRIEVED → EVENT → DECISION。
func TestAdapterDynamicMapping(t *testing.T) {
	cc := sampleCompiled()
	ad := NewOpenAICompatibleAdapter()
	msgs, err := ad.CompileMessages(context.Background(), cc)
	if err != nil {
		t.Fatalf("CompileMessages: %v", err)
	}
	user := msgs[1].Content

	// 四段都应出现。
	for _, want := range []string{"CURRENT STATE", "RETRIEVED CONTEXT", "EVENTS", "DECISION OPTIONS"} {
		if !strings.Contains(user, want) {
			t.Errorf("user message missing section %q\nuser=%q", want, user)
		}
	}
	// 顺序断言：用 index 验证固定顺序。
	posState := strings.Index(user, "CURRENT STATE")
	posRetr := strings.Index(user, "RETRIEVED CONTEXT")
	posEvent := strings.Index(user, "EVENTS")
	posDec := strings.Index(user, "DECISION OPTIONS")
	if !(posState < posRetr && posRetr < posEvent && posEvent < posDec) {
		t.Errorf("user section order wrong: state=%d retr=%d event=%d dec=%d (want ascending)",
			posState, posRetr, posEvent, posDec)
	}
}

// TestAdapterDeterministic 验证：同一 CompiledContext 多次编译得到完全一致的消息
// 顺序与内容。这是未来 KV Cache 命中的前提。
func TestAdapterDeterministic(t *testing.T) {
	cc := sampleCompiled()
	ad := NewOpenAICompatibleAdapter()
	var prev []string
	for i := 0; i < 3; i++ {
		msgs, err := ad.CompileMessages(context.Background(), cc)
		if err != nil {
			t.Fatalf("iter %d CompileMessages: %v", i, err)
		}
		var cur []string
		for _, m := range msgs {
			cur = append(cur, m.Role+"::"+m.Content)
		}
		if i == 0 {
			prev = cur
			continue
		}
		if len(cur) != len(prev) {
			t.Fatalf("iter %d: message count changed %d -> %d", i, len(prev), len(cur))
		}
		for j := range cur {
			if cur[j] != prev[j] {
				t.Errorf("iter %d msg[%d] differs:\n  prev=%q\n  cur =%q", i, j, prev[j], cur[j])
			}
		}
		prev = cur
	}
}

// TestAdapterProviderIndependent 验证：ContextEngine 不需要知道具体 Provider。
// 用 FakeAdapter 模拟任意 Provider，证明只要满足 ContextAdapter 接口即可消费
// CompiledContext，且 Runtime 状态未被 Adapter 修改（单向依赖）。
func TestAdapterProviderIndependent(t *testing.T) {
	cc := sampleCompiled()
	// 记录调用前 Runtime 事实，断言 Adapter 后未被篡改。
	beforeStable, beforeDyn, beforeDec := len(cc.StableBlocks), len(cc.DynamicBlocks), len(cc.DecisionBlocks)

	fake := NewFakeAdapter("provider-X")
	msgs, err := fake.CompileMessages(context.Background(), cc)
	if err != nil {
		t.Fatalf("fake CompileMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" {
		t.Fatalf("fake adapter should emit 1 user msg, got %+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "provider-X") {
		t.Errorf("fake adapter content missing provider tag: %q", msgs[0].Content)
	}

	// 单向依赖断言：Adapter 没有改 Runtime 事实。
	if len(cc.StableBlocks) != beforeStable || len(cc.DynamicBlocks) != beforeDyn || len(cc.DecisionBlocks) != beforeDec {
		t.Errorf("Adapter mutated CompiledContext: stable %d->%d dyn %d->%d dec %d->%d",
			beforeStable, len(cc.StableBlocks), beforeDyn, len(cc.DynamicBlocks), beforeDec, len(cc.DecisionBlocks))
	}

	// 同样的 CompiledContext 喂给另一个 Provider 模拟（OpenAICompatible），互不干扰。
	real := NewOpenAICompatibleAdapter()
	realMsgs, err := real.CompileMessages(context.Background(), cc)
	if err != nil {
		t.Fatalf("real CompileMessages: %v", err)
	}
	if len(realMsgs) != 2 {
		t.Fatalf("real adapter should emit 2 msgs, got %d", len(realMsgs))
	}
	// FakeAdapter 与 OpenAICompatibleAdapter 都从同一 CompiledContext 读取，互不影响。
	if len(cc.StableBlocks) != beforeStable {
		t.Errorf("second adapter call mutated CompiledContext")
	}
}
