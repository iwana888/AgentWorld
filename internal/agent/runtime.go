package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentworld/internal/a2a"
	"agentworld/internal/bus"
	"agentworld/internal/capability"
	"agentworld/internal/db"
	"agentworld/internal/federation"
	"agentworld/internal/llm"
	"agentworld/internal/logx"
	"agentworld/internal/models"
	"agentworld/internal/world"
	"agentworld/sdk"

	"gorm.io/gorm"
)

// Runtime Agent 运行时：加载上下文 → 模块决策 → 执行动作。
// Runtime 是框架的"编排器"，不持有任何业务语义；具体行为由注入的 Module 决定。
type Runtime struct {
	DB             *gorm.DB
	LLM            *llm.Client
	Bus            *bus.Broker
	DailyPostLimit int                    // 每角色每日发帖上限（0=不限制）
	GoalEnabled    bool                   // 是否启用 Agent 自主目标（Goal）驱动行为
	modules        map[string]Module      // 世界名 → 场景模块（多世界共存）
	moduleMu       sync.RWMutex           // 保护 modules 的并发读写（Scheduler 多 goroutine 调 Think）
	World          *world.Engine          // World Engine（M6）：世界主动变化，Agent 感知
	Capabilities   *capability.Registry   // M9：Capability 注册表（Agent 可调用外部工具）
	A2A            *a2a.Bus               // M12：A2A 消息总线（Agent 间通信，独立于 SSE bus）
	Fed            *federation.Client     // M12.4：Federation 客户端（跨实例发送 / 分布式通讯录）
	FedEndpoint    *federation.Endpoint   // M12.4：Federation 服务端（接收远端消息 / 暴露 Manifest）
	sdkrt          sdk.Runtime            // M11：缓存的 sdk.Runtime 上下文（Module 通信契约）
}

func NewRuntime(d *gorm.DB, l *llm.Client, b *bus.Broker) *Runtime {
	return &Runtime{
		DB:           d,
		LLM:          l,
		Bus:          b,
		modules:      map[string]Module{},
		Capabilities: capability.NewRegistry(),
		A2A:          a2a.NewBus(d),
	}
}

// toolActionPrefix 能力调用动作前缀。决策 Action 形如 "tool:send_room_key"，
// 表示"调用某 Capability 工具"。Runtime 不解释工具细节，统一交给 Capabilities 执行。
const toolActionPrefix = "tool:"

// tryToolAction 尝试把决策解释为一次能力（外部工具）调用。
// 返回 (执行结果, 是否命中能力)。命中即执行外部工具并写回结果。
// dec.Arguments（JSON 扩展字段）携带工具参数；若为空，用 dec.Content 作为单参数输入。
func (r *Runtime) tryToolAction(a models.Agent, dec *sdk.Decision) (string, bool) {
	if !strings.HasPrefix(dec.Action, toolActionPrefix) {
		return "", false
	}
	toolName := strings.TrimPrefix(dec.Action, toolActionPrefix)
	args := map[string]interface{}{}
	if dec.ToolArgs != nil && len(dec.ToolArgs) > 0 {
		args = dec.ToolArgs
	} else if dec.Content != "" {
		args["input"] = dec.Content
	}
	if r.Capabilities == nil {
		return "能力系统未启用", true
	}
	// 在整个注册表中查找该工具（按工具名跨能力匹配）
	var tool *capability.Tool
	for _, c := range r.Capabilities.List() {
		if t := c.FindTool(toolName); t != nil {
			tool = t
			break
		}
	}
	if tool == nil {
		return fmt.Sprintf("未知工具 %q，请先注册 Capability", toolName), true
	}
	out, err := tool.Execute(args)
	_ = db.RecordAction(r.DB, models.AgentAction{
		AgentID:    a.ID,
		Action:     dec.Action,
		TargetType: "tool",
		Input:      jsonArgsToString(args),
		Output:     out,
		Thought:    dec.Reason,
	})
	if err != nil {
		return "工具调用失败: " + err.Error(), true
	}
	// 能力调用结果写回 Agent 记忆（重要结果才记）
	if dec.Importance > 0 {
		dec.Memory = "调用外部工具 " + toolName + " 返回：" + truncate(out, 120)
		dec.MemoryType = "self"
		r.SaveMemory(a, toInternalDecision(dec))
	}
	return out, true
}

func jsonArgsToString(args map[string]interface{}) string {
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprintf("%v", args)
	}
	return string(b)
}

// WithModule 注入自定义场景模块（兼容单模块用法，注册到默认世界 social）。
func (r *Runtime) WithModule(m Module) *Runtime {
	return r.RegisterModule("social", m)
}

// RegisterModule 注册一个世界模块，world 为该世界名（对应 Agent.World）。
// 多个世界可共存，Think 会按 Agent.World 分派到对应模块。
func (r *Runtime) RegisterModule(world string, m Module) *Runtime {
	r.moduleMu.Lock()
	r.modules[world] = m
	r.moduleMu.Unlock()
	return r
}

// module 取某 Agent 所属世界的模块；未注册则懒加载内置 SocialModule。
// 注意：Scheduler 会对多个 Agent 并发调用 Think → 本方法会被并发访问，
// 懒加载分支必须加锁，否则 Go map 并发写会 fatal error。
func (r *Runtime) module(a models.Agent) Module {
	world := a.World
	if world == "" {
		world = "social"
	}
	// 读路径用 RLock（大多数 Agent 的世界已注册，只读）
	r.moduleMu.RLock()
	m, ok := r.modules[world]
	r.moduleMu.RUnlock()
	if ok {
		return m
	}
	// 未注册该世界 → 默认用社交模块兜底（写路径加写锁，避免并发写 map）
	r.moduleMu.Lock()
	defer r.moduleMu.Unlock()
	// 双检：等待写锁期间可能已被其他 goroutine 注册
	if m, ok := r.modules[world]; ok {
		return m
	}
	m = NewSocialModule(r.SDK(), r.LLM)
	r.modules[world] = m
	return m
}

// SetDailyPostLimit 设置每角色每日发帖上限
func (r *Runtime) SetDailyPostLimit(n int) { r.DailyPostLimit = n }

// SetGoalEnabled 设置是否启用 Agent 自主目标驱动行为
func (r *Runtime) SetGoalEnabled(on bool) { r.GoalEnabled = on }

// Think 唤醒一个 Agent 完成一次自主思考-行动循环（框架主流程）。
// 流程：限流检查 → 模块感知 → 模块决策 → 模块执行。业务细节全部在 Module 内。
func (r *Runtime) Think(ctx context.Context, a models.Agent) {
	// 每日发帖上限：已达上限则跳过本次（仍记入 agent_actions 便于观察节流）
	if r.DailyPostLimit > 0 {
		if n, err := db.CountPostsToday(r.DB, a.ID); err == nil && n >= int64(r.DailyPostLimit) {
			_ = db.RecordAction(r.DB, models.AgentAction{
				AgentID: a.ID, Action: "skip", TargetType: "daily-limit",
				Output: "已达每日发帖上限", Thought: "节流：今日已发 " + fmt.Sprint(n) + " 帖",
			})
			return
		}
	}

	m := r.module(a)

	// M11：Runtime 与 Module 只通过 sdk.Runtime / sdk.Module 通信。
	// 把内部 models.Agent 转为 sdk.Agent，并注入 sdk.Runtime 上下文（不透传 *Runtime）。
	sa := toSDKAgent(a)
	sdkrt := newSDKRuntime(r)

	// 1) 感知：构造该 Agent 本轮所见世界视图
	perc, err := m.Perceive(ctx, sa)
	if err != nil {
		perc = nil
	}

	// 2) 决策：把感知转换为结构化动作（nil 表示本轮不动作）
	dec, _ := m.Planner().Decide(ctx, sa, perc)
	if dec == nil {
		return
	}

	// 3) M9 能力路由：若决策是"调用外部工具"，交给 Capability 执行，跳过常规执行
	if out, hit := r.tryToolAction(a, dec); hit {
		fields := logx.F{"agent": a.Name, "id": a.ID, "action": dec.Action, "tool_result": truncate(out, 60)}
		logx.D("capability", fields)
		return
	}

	// 4) 执行：把决策落到共享世界（通过 sdk.Runtime 上下文，不传 *Runtime）
	_, _ = m.Executor().Execute(ctx, sdkrt, sa, perc, dec)

	// 行为日志（结构化字段，便于 grep 排查单个 Agent 的行为）
	fields := logx.F{"agent": a.Name, "id": a.ID, "action": dec.Action}
	if dec.Target != 0 {
		fields["target"] = dec.Target
		fields["target_kind"] = dec.TargetKind
	}
	if dec.Content != "" {
		fields["content"] = truncate(dec.Content, 40)
	}
	logx.D("think", fields)
}

// shouldUseLLM 该 Agent 是否应调用真实 LLM：必须全局已配置 key 且 Agent 本身开启 use_llm
func (r *Runtime) shouldUseLLM(a models.Agent) bool {
	return r.LLM != nil && r.LLM.Enabled() && a.UseLLM
}

func now() string { return time.Now().Format("15:04:05") }
