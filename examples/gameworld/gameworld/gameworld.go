// Package gameworld —— M10 SDK 示例：一个可被 AgentWorld 运行时调度的"打怪升级"世界。
//
// 演示如何仅依赖 agentworld/sdk 包、不接触 internal/*，实现一个第三方 Module：
//  1. 实现 sdk.Module（GameWorld）：Name / Perceive / Planner / Executor / WakePolicy / OnBoot
//  2. 导出 New() 返回 sdk.Module，供宿主运行时注册调度。
package gameworld

import (
	"context"

	"agentworld/sdk"
)

// GameWorld 一个极简"打怪升级"世界：Agent 每轮感知状态，决策"打怪/休息"。
type GameWorld struct {
	rt sdk.Runtime
}

// New 构造一个 Game 世界模块，供 AgentWorld 运行时注册（第三方 SDK 扩展示例）。
func New() sdk.Module {
	return &GameWorld{}
}

// 感知：告诉 Agent 当前状态。
type gamePerception struct {
	Level int
	HP    int
}

func (m *GameWorld) Name() string { return "gameworld" }

func (m *GameWorld) OnBoot(rt sdk.Runtime) error {
	m.rt = rt
	return rt.DB().Exec(`CREATE TABLE IF NOT EXISTS g_hero (
		agent_id INTEGER PRIMARY KEY,
		level INTEGER DEFAULT 1,
		hp INTEGER DEFAULT 100
	)`).Error
}

func (m *GameWorld) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
	var lvl, hp int
	row := m.rt.DB().Raw("SELECT level, hp FROM g_hero WHERE agent_id = ?", a.ID).Row()
	if row == nil || row.Scan(&lvl, &hp) != nil {
		lvl, hp = 1, 100
		m.rt.DB().Exec("INSERT OR REPLACE INTO g_hero(agent_id, level, hp) VALUES(?,?,?)", a.ID, lvl, hp)
	}
	return gamePerception{Level: lvl, HP: hp}, nil
}

// 决策器（规则式，无需 LLM）。
type gamePlanner struct{}

func (p gamePlanner) Decide(ctx context.Context, a sdk.Agent, perc sdk.Perception) (*sdk.Decision, error) {
	g := perc.(gamePerception)
	if g.HP > 30 {
		return &sdk.Decision{Action: "fight", Reason: "状态良好，继续打怪"}, nil
	}
	return &sdk.Decision{Action: "rest", Reason: "血量不足，休息恢复"}, nil
}

// 执行器：落实决策，并调用能力（天气）与状态系统。
type gameExecutor struct{ m *GameWorld }

func (e gameExecutor) Execute(ctx context.Context, rt sdk.Runtime, a sdk.Agent, perc sdk.Perception, dec *sdk.Decision) (string, error) {
	switch dec.Action {
	case "fight":
		rt.DB().Exec("UPDATE g_hero SET level = level + 1, hp = hp - 10 WHERE agent_id = ?", a.ID)
		if out, err := rt.CallTool("weather", "get_weather", map[string]interface{}{}); err == nil && out != "" {
			return "打怪升级，顺便查了下天气：" + out, nil
		}
		return "打怪升级，等级 +1", nil
	case "rest":
		rt.DB().Exec("UPDATE g_hero SET hp = 100 WHERE agent_id = ?", a.ID)
		_ = rt.ApplyStateDelta(a, sdk.StateDelta{Energy: 10, NeedEntertainment: 5})
		return "休息回满血", nil
	}
	return "无动作", nil
}

func (m *GameWorld) Planner() sdk.Planner   { return gamePlanner{} }
func (m *GameWorld) Executor() sdk.Executor { return gameExecutor{m} }
func (m *GameWorld) WakePolicy() sdk.WakePolicy {
	// 使用 SDK 官方提供的唤醒策略。
	return sdk.NewAlwaysWakePolicy()
}
