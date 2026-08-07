package agent

import (
	"context"
	"time"

	"agentworld/internal/a2a"
	"agentworld/internal/bus"
	"agentworld/internal/db"
	"agentworld/internal/federation"
	"agentworld/internal/llm"
	"agentworld/internal/models"
	"agentworld/sdk"

	"gorm.io/gorm"
)

// 本文件实现 M11：Runtime 通过 sdk.Runtime / sdk.Module 与官方/第三方 Module 通信。
// 由于内部接口（Module/Planner/Executor/WakePolicy）已 type alias 到 sdk 类型，
// 官方 Social/Hotel 与第三方 sdk.Module 使用同一套接口，无需桥接适配器。
// 本文件仅保留：类型转换函数 + sdkRuntimeAdapter（sdk.Runtime 上下文实现）。

// ---- 类型转换 ----

func toSDKAgent(a models.Agent) sdk.Agent {
	return sdk.Agent{
		ID:           a.ID,
		Name:         a.Name,
		World:        a.World,
		Kind:         a.Kind,
		SystemPrompt: a.SystemPrompt,
		Goal:         a.Goal,
		UseLLM:       a.UseLLM,
		Interests:    a.Interests,
		Extra:        a,
	}
}

func toSDKAgents(list []models.Agent) []sdk.Agent {
	return ToSDKAgents(list)
}

func toInternalAgents(list []sdk.Agent) []models.Agent {
	return FromSDKAgents(list)
}

// ToSDKAgents / FromSDKAgents 导出：供 scheduler 等外部包在 sdk.Agent 与内部模型间转换。
func ToSDKAgents(list []models.Agent) []sdk.Agent {
	out := make([]sdk.Agent, 0, len(list))
	for _, a := range list {
		out = append(out, toSDKAgent(a))
	}
	return out
}

func FromSDKAgents(list []sdk.Agent) []models.Agent {
	out := make([]models.Agent, 0, len(list))
	for _, a := range list {
		out = append(out, fromSDKAgent(a))
	}
	return out
}

func fromSDKAgent(a sdk.Agent) models.Agent {
	if m, ok := a.Extra.(models.Agent); ok {
		return m
	}
	return models.Agent{
		ID:           a.ID,
		Name:         a.Name,
		World:        a.World,
		Kind:         a.Kind,
		SystemPrompt: a.SystemPrompt,
		Goal:         a.Goal,
		UseLLM:       a.UseLLM,
		Interests:    a.Interests,
	}
}

func toSDKDecision(d *llm.Decision) *sdk.Decision {
	if d == nil {
		return nil
	}
	return &sdk.Decision{
		Action:     d.Action,
		Target:     d.Target,
		TargetKind: d.TargetKind,
		Content:    d.Content,
		Reason:     d.Reason,
		Memory:     d.Memory,
		MemoryType: d.MemoryType,
		Importance: d.Importance,
		ToolArgs:   d.ToolArgs,
	}
}

func toSDKStateDelta(d StateDelta) sdk.StateDelta {
	return sdk.StateDelta{
		Mood:              d.Mood,
		Energy:            d.Energy,
		Curiosity:         d.Curiosity,
		SocialNeed:        d.SocialNeed,
		NeedSocial:        d.NeedSocial,
		NeedKnowledge:     d.NeedKnowledge,
		NeedAchievement:   d.NeedAchievement,
		NeedEntertainment: d.NeedEntertainment,
		Attention:         d.Attention,
		Var:               d.Var,
	}
}

func toInternalDecision(d *sdk.Decision) *llm.Decision {
	if d == nil {
		return nil
	}
	return &llm.Decision{
		Action:     d.Action,
		Target:     d.Target,
		TargetKind: d.TargetKind,
		Content:    d.Content,
		Reason:     d.Reason,
		Memory:     d.Memory,
		MemoryType: d.MemoryType,
		Importance: d.Importance,
		ToolArgs:   d.ToolArgs,
	}
}

// ---- sdk.Runtime 实现 ----

// sdkRuntimeAdapter 实现 sdk.Runtime 接口，把内部 *Runtime 的能力以 sdk 契约暴露给 Module。
// Module（官方或第三方）只能看到 sdk.Runtime，不能访问 *Runtime 内部字段。
type sdkRuntimeAdapter struct {
	rt *Runtime
}

func newSDKRuntime(rt *Runtime) sdk.Runtime {
	return &sdkRuntimeAdapter{rt: rt}
}

// SDK 返回 Runtime 的 sdk.Runtime 上下文（M11 通信契约）。
// 官方与第三方 Module 都通过它访问运行时能力，不接触 *Runtime。
func (r *Runtime) SDK() sdk.Runtime {
	if r.sdkrt == nil {
		r.sdkrt = &sdkRuntimeAdapter{rt: r}
	}
	return r.sdkrt
}

func (a *sdkRuntimeAdapter) DB() *gorm.DB {
	if a.rt == nil {
		return nil
	}
	return a.rt.DB
}

func (a *sdkRuntimeAdapter) SaveMemory(ag sdk.Agent, dec *sdk.Decision) {
	if a.rt == nil || dec == nil {
		return
	}
	a.rt.SaveMemory(fromSDKAgent(ag), toInternalDecision(dec))
}

func (a *sdkRuntimeAdapter) ApplyStateDelta(ag sdk.Agent, d sdk.StateDelta) error {
	if a.rt == nil {
		return nil
	}
	_, err := a.rt.ApplyStateDelta(fromSDKAgent(ag), StateDelta{
		Mood:              d.Mood,
		Energy:            d.Energy,
		Curiosity:         d.Curiosity,
		SocialNeed:        d.SocialNeed,
		NeedSocial:        d.NeedSocial,
		NeedKnowledge:     d.NeedKnowledge,
		NeedAchievement:   d.NeedAchievement,
		NeedEntertainment: d.NeedEntertainment,
		Attention:         d.Attention,
		Var:               d.Var,
	})
	return err
}

func (a *sdkRuntimeAdapter) LoadState(ag sdk.Agent) (interface{}, error) {
	if a.rt == nil {
		return nil, nil
	}
	return a.rt.LoadState(fromSDKAgent(ag))
}

func (a *sdkRuntimeAdapter) PublishEvent(e interface{}) {
	if a.rt == nil || a.rt.Bus == nil {
		return
	}
	if ev, ok := e.(bus.Event); ok {
		a.rt.Bus.Publish(ev)
	}
}

func (a *sdkRuntimeAdapter) CallTool(capabilityName, tool string, args map[string]interface{}) (string, error) {
	if a.rt == nil || a.rt.Capabilities == nil {
		return "", nil
	}
	c := a.rt.Capabilities.Get(capabilityName)
	if c == nil {
		return "", nil
	}
	t := c.FindTool(tool)
	if t == nil {
		return "", nil
	}
	return t.Execute(args)
}

func (a *sdkRuntimeAdapter) CapabilityNames() []string {
	if a.rt == nil || a.rt.Capabilities == nil {
		return nil
	}
	var names []string
	for _, c := range a.rt.Capabilities.List() {
		names = append(names, c.Name)
	}
	return names
}

func (a *sdkRuntimeAdapter) UseLLM(ag sdk.Agent) bool {
	if a.rt == nil {
		return false
	}
	return a.rt.shouldUseLLM(fromSDKAgent(ag))
}

func (a *sdkRuntimeAdapter) GoalEnabled() bool {
	return a.rt != nil && a.rt.GoalEnabled
}

func (a *sdkRuntimeAdapter) WorldEvents(since time.Duration) []sdk.Event {
	if a.rt == nil || a.rt.World == nil {
		return nil
	}
	evs := a.rt.World.Events(time.Now().Add(-since))
	out := make([]sdk.Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, sdk.Event{
			Type:      e.Type,
			Title:     e.Title,
			Detail:    e.Detail,
			TargetTag: e.TargetTag,
			CreatedAt: e.CreatedAt,
		})
	}
	return out
}

func (a *sdkRuntimeAdapter) Send(msg sdk.Message) error {
	if a.rt == nil || a.rt.A2A == nil {
		return nil
	}
	return a.rt.A2A.Send(msg)
}

func (a *sdkRuntimeAdapter) Inbox(agentID int64, status string) []sdk.Message {
	if a.rt == nil || a.rt.A2A == nil {
		return nil
	}
	return a.rt.A2A.Inbox(agentID, status)
}

func (a *sdkRuntimeAdapter) MarkMessage(id int64, status string) error {
	if a.rt == nil || a.rt.A2A == nil {
		return nil
	}
	return a.rt.A2A.Mark(id, status)
}

func (a *sdkRuntimeAdapter) Discover(skill string) []sdk.AgentRef {
	if a.rt == nil || a.rt.A2A == nil {
		return nil
	}
	return a.toSDKRefs(a.rt.A2A.Discover(skill))
}

func (a *sdkRuntimeAdapter) Select(from int64, skill string) []sdk.AgentRef {
	if a.rt == nil || a.rt.A2A == nil {
		return nil
	}
	return a.toSDKRefs(a.rt.A2A.Select(from, skill))
}

// toSDKRefs 把内部 AgentRef 转为 sdk.AgentRef 并补名字。
func (a *sdkRuntimeAdapter) toSDKRefs(refs []a2a.AgentRef) []sdk.AgentRef {
	out := make([]sdk.AgentRef, 0, len(refs))
	for _, r := range refs {
		ref := sdk.AgentRef{
			AgentID:      r.AgentID,
			World:        r.World,
			Skill:        r.Skill,
			Score:        r.Score,
			Fitness:      r.Fitness,
			Relationship: r.Relationship,
			SuccessRate:  r.SuccessRate,
		}
		if sa, err := db.GetAgent(a.rt.DB, r.AgentID); err == nil {
			ref.Name = sa.Name
		}
		out = append(out, ref)
	}
	return out
}

func (a *sdkRuntimeAdapter) SendRemote(ctx context.Context, ref sdk.RemoteRef, msg sdk.RemoteMessage) error {
	if a.rt == nil || a.rt.Fed == nil {
		return nil
	}
	_, err := a.rt.Fed.SendRemote(ctx, federation.RemoteAddr{
		Endpoint: ref.Endpoint,
		World:    ref.World,
		AgentID:  ref.AgentID,
	}, federation.RemoteMessage{
		Intent:        msg.Intent,
		Payload:       msg.Payload,
		ReplyTo:       msg.ReplyTo,
		CorrelationID: msg.CorrelationID,
		From: federation.FromRef{
			World: msg.From.World,
			Agent: msg.From.Agent,
		},
	})
	return err
}

func (a *sdkRuntimeAdapter) DiscoverRemote(ctx context.Context, endpoint string) error {
	if a.rt == nil || a.rt.Fed == nil {
		return nil
	}
	_, err := a.rt.Fed.DiscoverRemote(ctx, endpoint)
	return err
}

func (a *sdkRuntimeAdapter) RemoteAgents(skill string) []sdk.RemoteRef {
	if a.rt == nil || a.rt.Fed == nil {
		return nil
	}
	addrs := a.rt.Fed.RemoteAgents(skill)
	out := make([]sdk.RemoteRef, 0, len(addrs))
	for _, ad := range addrs {
		out = append(out, sdk.RemoteRef{
			Endpoint: ad.Endpoint,
			World:    ad.World,
			AgentID:  ad.AgentID,
			Skill:    skill,
		})
	}
	return out
}

func (a *sdkRuntimeAdapter) Capabilities() []sdk.CapabilityInfo {
	if a.rt == nil || a.rt.Capabilities == nil {
		return nil
	}
	var out []sdk.CapabilityInfo
	for _, c := range a.rt.Capabilities.List() {
		info := sdk.CapabilityInfo{Name: c.Name, Desc: c.Desc}
		for _, t := range c.Tools {
			tool := sdk.ToolInfo{Name: t.Name, Description: t.Description}
			for _, p := range t.Parameters {
				tool.Parameters = append(tool.Parameters, sdk.ParamInfo{
					Name:        p.Name,
					Type:        p.Type,
					Description: p.Description,
					Required:    p.Required,
					Default:     p.Default,
				})
			}
			info.Tools = append(info.Tools, tool)
		}
		out = append(out, info)
	}
	return out
}

// 保留 sdk.Module 注册入口（Runtime.RegisterSDKModule），与第三方一致。
// 官方模块同样通过该入口注册，验证"官方与第三方同一套机制"。
// 返回 sdk.Module，便于调用方继续做 OnBoot。
func (r *Runtime) RegisterSDKModule(m sdk.Module) sdk.Module {
	r.RegisterModule(m.Name(), m)
	return m
}
