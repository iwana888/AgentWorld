package context

import (
	stdctx "context"
	"fmt"
	"strings"

	"agentworld/internal/llm"
)

// ContextAdapter 把 Runtime 的事实（CompiledContext）转成 Provider 的表现形式
// （[]llm.Message）。它是 Context Runtime 与 LLM Provider 之间唯一的分界点。
//
// 上下游单向依赖：
//
//	ContextEngine ──► CompiledContext ──► ContextAdapter ──► Provider
//
// 关键边界（M8.9 要验证）：
//   - ContextEngine / CompiledContext 完全不知道 OpenAI / DeepSeek / Claude。
//   - Adapter 只能"读" CompiledContext，绝不能反过来修改 ContextBlock /
//     Budget / Retriever / Compactor。Adapter 是无副作用的投影（pure projection）。
//   - 第一版只做固定映射，不引入任何 Provider 专属逻辑（尤其不要写 DeepSeek
//     Cache 逻辑——那是 M8.9 明确不做的）。
//
// 固定映射规则（第一版可固化，未来如需按 Provider 调整可新增 Adapter 实现）：
//
//	system:  STABLE 全部（rules / identity / personality / tool / skill）
//	user:    STATE ─ RETRIEVED ─ EVENT ─ DECISION（按此顺序拼成一条 user 消息）
//
// Stable 永远是稳定前缀，对未来 KV Cache 命中至关重要（同 Agent 多轮 Think
// 的 system 内容不变 → Cache 命中）。
type ContextAdapter interface {
	CompileMessages(ctx stdctx.Context, c *CompiledContext) ([]llm.Message, error)
}

// OpenAICompatibleAdapter 把 CompiledContext 投影为 OpenAI-compatible 的消息数组。
// 由于现有 llm.Client 本身就是 OpenAI-compatible（DeepSeek / 多数国产模型都兼容），
// 这个 Adapter 同时覆盖 OpenAI / DeepSeek / Claude(兼容模式) 等 Provider。
//
// 不出现任何 Cache / Provider 专属字段——保持极简。
type OpenAICompatibleAdapter struct{}

// NewOpenAICompatibleAdapter 构造 Adapter。
func NewOpenAICompatibleAdapter() *OpenAICompatibleAdapter {
	return &OpenAICompatibleAdapter{}
}

// CompileMessages 实现 ContextAdapter。
// 生成 2 条消息：1 条 system（STABLE）+ 1 条 user（STATE/RETRIEVED/EVENT/DECISION）。
// 顺序完全固定、确定，保证同输入同输出（KV Cache 友好）。
func (a *OpenAICompatibleAdapter) CompileMessages(ctx stdctx.Context, c *CompiledContext) ([]llm.Message, error) {
	if c == nil {
		return nil, fmt.Errorf("context: adapter nil CompiledContext")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	system := buildSystem(c.StableBlocks)
	user := buildUser(c)

	// 顺序固定：system 必须在前，user 在后。这是 Provider 协议的硬约束。
	msgs := []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
	return msgs, nil
}

// buildSystem 把 STABLE 块拼成 system 文本。Stable 永远是稳定前缀。
func buildSystem(stable []ContextBlock) string {
	var sb strings.Builder
	for _, b := range stable {
		// 用 source 作为小标题分隔，便于模型理解结构；空 source 直接拼接内容。
		if b.Source != "" {
			sb.WriteString("# ")
			sb.WriteString(b.Source)
			sb.WriteString("\n")
		}
		sb.WriteString(b.Content)
		sb.WriteString("\n\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// buildUser 把 Dynamic（STATE/RETRIEVED/EVENT）与 DECISION 拼成 user 文本。
// 顺序固定：STATE → RETRIEVED → EVENT → DECISION。
func buildUser(c *CompiledContext) string {
	var sb strings.Builder

	var state, retrieved, event []ContextBlock
	for _, b := range c.DynamicBlocks {
		switch b.Type {
		case TypeAgentState, TypeWorldState:
			state = append(state, b)
		case TypeRetrieved:
			retrieved = append(retrieved, b)
		case TypeEvent:
			event = append(event, b)
		default:
			// 未知 Dynamic 类型归入 state 段，避免信息丢失。
			state = append(state, b)
		}
	}

	writeSection := func(title string, blocks []ContextBlock) {
		if len(blocks) == 0 {
			return
		}
		sb.WriteString("## " + title + "\n")
		for _, b := range blocks {
			if b.Source != "" {
				sb.WriteString("[" + b.Source + "]\n")
			}
			sb.WriteString(b.Content)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	writeSection("CURRENT STATE", state)
	writeSection("RETRIEVED CONTEXT", retrieved)
	writeSection("EVENTS", event)
	writeSection("DECISION OPTIONS", c.DecisionBlocks)

	return strings.TrimRight(sb.String(), "\n")
}

// FakeAdapter 测试用假 Adapter。
// 它证明 ContextEngine 根本不需要知道具体 Provider——只要满足 ContextAdapter
// 接口，任何实现都能被 Runtime 消费。FakeAdapter 不改变任何 Runtime 状态，
// 仅把 CompiledContext 的块数投影成一条标记消息，方便断言"上下游单向依赖"。
type FakeAdapter struct {
	name string
}

// NewFakeAdapter 构造测试用假 Adapter。name 用于区分不同 Provider 模拟。
func NewFakeAdapter(name string) *FakeAdapter {
	if name == "" {
		name = "fake"
	}
	return &FakeAdapter{name: name}
}

// CompileMessages 实现 ContextAdapter，仅输出一条汇总的 user 消息，
// 内容包含块计数，便于在测试中验证"Adapter 能拿到全部块但没改它们"。
func (a *FakeAdapter) CompileMessages(ctx stdctx.Context, c *CompiledContext) ([]llm.Message, error) {
	if c == nil {
		return nil, fmt.Errorf("context: fake adapter nil CompiledContext")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	content := fmt.Sprintf("[%s] stable=%d dynamic=%d decision=%d total=%d",
		a.name, len(c.StableBlocks), len(c.DynamicBlocks), len(c.DecisionBlocks), c.TokenUsage.Total)
	return []llm.Message{{Role: "user", Content: content}}, nil
}
