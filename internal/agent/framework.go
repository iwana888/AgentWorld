package agent

import (
	"context"
	"encoding/json"
	"strings"

	"agentworld/internal/bus"
	"agentworld/internal/db"
	"agentworld/internal/llm"
	"agentworld/internal/models"
	"agentworld/sdk"
)

// 本文件定义 AgentWorld 的多 Agent 框架抽象层。
//
// 设计目标：把 agentworld 从“一个微博模拟器”升级为“可复用的多 Agent 协同沙盒”。
// 框架只负责调度与编排（Scheduler / Runtime），具体的“感知-决策-执行”逻辑
// 由可插拔的模块（Module）承载。内置的 SocialModule 即原来的微博模拟逻辑，
// 作为默认场景保留；用户可自定义 Module 实现任意协同行为（任务流、博弈、
// 信息扩散等），无需改动调度与编排代码。

// Module 一个场景/世界的所有可插拔行为都收敛到一个 Module 中。
// 框架只依赖 Module 接口，不关心内部细节。这样同一套调度器可以驱动
// 微博模拟、任务编排、市场模拟等完全不同的 Agent 世界。
//
// M11：官方 Module 与第三方 Module 使用同一套接口（sdk.Module），
// Runtime 与 Module 之间只通过 sdk.Runtime / sdk.Module 通信，不传内部 *Runtime。
type Module = sdk.Module

// PerceptionForSocial 是 SocialModule 使用的感知类型（即 prompt 文本）。
// 单独命名仅为了可读性；框架层按 string 透传。
type PerceptionForSocial = string

// 类型别名：官方 Module 与第三方 Module 使用同一套 sdk 接口。
// 这些别名让 internal/agent 内部代码（Social/Hotel）引用 agent.Perception 等时，
// 直接映射到 sdk 类型，保证"官方与第三方完全同权"。
type Perception = sdk.Perception
type Planner = sdk.Planner
type Executor = sdk.Executor
type WakePolicy = sdk.WakePolicy

// LLMPlanner 基于真实大模型的 Planner 实现（默认决策器之一）。
// 若 Agent 未开启 use_llm 或 LLM 未配置，Decide 返回 nil，由调用方回退。
// M11：通过 sdk.Runtime.UseLLM 判断，不再直接持有 *Runtime。
type LLMPlanner struct {
	LLM *llm.Client
	RT  sdk.Runtime
}

// Decide 仅对开启 use_llm 的 Agent 调用真实 LLM；否则返回 nil。
func (p *LLMPlanner) Decide(ctx context.Context, a sdk.Agent, perc sdk.Perception) (*sdk.Decision, error) {
	if p.LLM == nil || !p.LLM.Enabled() || !a.UseLLM {
		return nil, nil
	}
	prompt := ""
	if s, ok := perc.(string); ok {
		prompt = s
	}
	dec, err := p.LLM.Decide(ctx, a.SystemPrompt, prompt)
	if err != nil || dec == nil {
		return nil, err
	}
	return &sdk.Decision{
		Action:     dec.Action,
		Target:     dec.Target,
		TargetKind: dec.TargetKind,
		Content:    dec.Content,
		Reason:     dec.Reason,
		Memory:     dec.Memory,
		MemoryType: dec.MemoryType,
		Importance: dec.Importance,
		ToolArgs:   dec.ToolArgs,
	}, nil
}

// 便捷构造：得到一个默认基于 LLM 的 Planner。
func NewLLMPlanner(llmClient *llm.Client, rt sdk.Runtime) *LLMPlanner {
	return &LLMPlanner{LLM: llmClient, RT: rt}
}

// SaveMemory 通用 helper：把一条记忆落库并裁剪到上限，供各 Module 复用。
// 非框架强制调用，仅作为内置能力的便捷封装。
func (r *Runtime) SaveMemory(a models.Agent, dec *llm.Decision) {
	m := strings.TrimSpace(dec.Memory)
	if m == "" {
		return
	}
	typ := dec.MemoryType
	if typ == "" {
		typ = "self"
	}
	imp := dec.Importance
	if imp <= 0 || imp > 5 {
		imp = 2
	}
	_ = db.AddMemory(r.DB, a.ID, typ, m, imp)
	_ = db.PruneMemories(r.DB, a.ID, 15)
}

// PublishEvent 通用 helper：向事件总线广播一条 Agent 行为，供前端实时监控。
func (r *Runtime) PublishEvent(e bus.Event) {
	if r.Bus != nil {
		r.Bus.Publish(e)
	}
}

// RecordAction 通用 helper：记录一次 Agent 行为到调试表。
// TargetType 直接用 dec.TargetKind（由 Module 决定，框架不解释）。
func (r *Runtime) RecordAction(a models.Agent, dec *llm.Decision, thought, output string) {
	_ = db.RecordAction(r.DB, models.AgentAction{
		AgentID:    a.ID,
		Action:     dec.Action,
		TargetType: dec.TargetKind,
		TargetID:   dec.Target,
		Input:      thought,
		Output:     output,
		Thought:    dec.Reason,
	})
}

// StateDelta 一次状态变化（M5）。由 Module 在事件后定义"这个事件改变了什么状态"。
// 例如社交的 like → {Mood:+2, SocialNeed:-5}；酒店的 checkin → {Energy:-10, Achievement:...}。
// 所有字段可选（0 表示不变），应用后自动 clamp 到合法区间。
type StateDelta struct {
	Mood       int
	Energy     int
	Curiosity  int
	SocialNeed int
	// M7：Need 变化（正值=需求上升，负值=需求得到满足下降）
	NeedSocial        int
	NeedKnowledge     int
	NeedAchievement   int
	NeedEntertainment int
	Attention  *string       // 非空则覆盖当前关注主题
	Var        map[string]any // 合并进 Variables
}

// LoadState 读取 Agent 状态（含自然衰减）。
func (r *Runtime) LoadState(a models.Agent) (*models.AgentState, error) {
	return db.GetState(r.DB, a.ID)
}

// ApplyStateDelta 把一次状态变化应用到 Agent（M5 核心能力）。
// 规则由 Module 决定，Runtime 只负责"应用 + clamp + 保存"。
func (r *Runtime) ApplyStateDelta(a models.Agent, delta StateDelta) (*models.AgentState, error) {
	st, err := db.GetState(r.DB, a.ID)
	if err != nil {
		return nil, err
	}
	st.Mood = clampInt(st.Mood+delta.Mood, -100, 100)
	st.Energy = clampInt(st.Energy+delta.Energy, 0, 100)
	st.Curiosity = clampInt(st.Curiosity+delta.Curiosity, 0, 100)
	st.SocialNeed = clampInt(st.SocialNeed+delta.SocialNeed, 0, 100)
	// M7：Need 维度（下降=满足，上升=累积）
	st.NeedSocial = clampInt(st.NeedSocial+delta.NeedSocial, 0, 100)
	st.NeedKnowledge = clampInt(st.NeedKnowledge+delta.NeedKnowledge, 0, 100)
	st.NeedAchievement = clampInt(st.NeedAchievement+delta.NeedAchievement, 0, 100)
	st.NeedEntertainment = clampInt(st.NeedEntertainment+delta.NeedEntertainment, 0, 100)
	if delta.Attention != nil {
		st.Attention = *delta.Attention
	}
	if len(delta.Var) > 0 {
		// 合并 Variables（JSON）
		merged := decodeVarMap(st.Variables)
		for k, v := range delta.Var {
			merged[k] = v
		}
		st.Variables = encodeVarMap(merged)
	}
	if err := db.SaveState(r.DB, st); err != nil {
		return nil, err
	}
	return st, nil
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// decodeVarMap 解析 Variables JSON 为 map；空则返回空 map。
func decodeVarMap(s string) map[string]any {
	m := map[string]any{}
	if s == "" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// encodeVarMap 编码 map 为 JSON。
func encodeVarMap(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
