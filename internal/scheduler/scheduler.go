// Package scheduler 负责"什么时候让 Agent 有机会思考"。
//
// M4 拆包：Scheduler 从 internal/agent 中拆出为独立包。它只依赖
// agent.Runtime 的 Think 方法与数据库查询，是单向依赖（scheduler → agent），
// 不产生循环依赖。职责单一：周期触发 + batch 限流 + 唤醒策略。
package scheduler

import (
	"context"
	"math/rand"
	"time"

	"agentworld/internal/agent"
	"agentworld/internal/db"
	"agentworld/internal/models"
)

// Scheduler 周期性唤醒 Agent。唤醒哪些 Agent 由注入的 WakePolicy 决定，
// 默认使用事件驱动策略（agent.EventWakePolicy）；同时把唤醒总数限制在 batch 区间。
type Scheduler struct {
	rt           *agent.Runtime
	interval     time.Duration
	batchMin     int
	batchMax     int
	customPolicy agent.WakePolicy // 注入的自定义激活策略（可选）
	idle         float64          // 默认事件策略的 idle 保底概率
}

// NewScheduler 构造调度器。
func NewScheduler(rt *agent.Runtime, interval time.Duration, batchMin, batchMax int) *Scheduler {
	return &Scheduler{
		rt:       rt,
		interval: interval,
		batchMin: batchMin,
		batchMax: batchMax,
		idle:     0.15, // 默认 15% 保底，既不太冷清也不浪费 token
	}
}

// SetWakePolicy 注入自定义激活策略；不调用则使用默认事件驱动策略。
func (s *Scheduler) SetWakePolicy(p agent.WakePolicy) {
	if p != nil {
		s.customPolicy = p
	}
}

// SetIdleWakeChance 设置事件驱动策略中"无事件 Agent 被保底唤醒"的概率（0~1）。
func (s *Scheduler) SetIdleWakeChance(chance float64) {
	if chance >= 0 && chance <= 1 {
		s.idle = chance
	}
}

// policy 取得激活策略：优先注入的，否则用内置事件驱动策略。
func (s *Scheduler) policy() agent.WakePolicy {
	if s.customPolicy != nil {
		return s.customPolicy
	}
	return &agent.EventWakePolicy{Rt: s.rt.SDK(), Chance: s.idle}
}

// Start 启动调度循环（阻塞于传入的 ctx）。
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.wake(ctx)
		}
	}
}

func (s *Scheduler) wake(ctx context.Context) {
	agents, err := db.ListAgents(s.rt.DB, "running")
	if err != nil || len(agents) == 0 {
		return
	}
	// M3：人类身份不参与 Agent 自主唤醒（只能由人类自己操作），
	// 排除 kind=human，避免人类账号被 Scheduler 驱动自主发帖。
	wakeable := agents[:0]
	for _, a := range agents {
		if a.Kind != "human" {
			wakeable = append(wakeable, a)
		}
	}
	agents = wakeable
	if len(agents) == 0 {
		return
	}

	// 1) 由激活策略选出候选（其内部已做事件优先 + idle 概率）
	// M11：Select 走 sdk.Runtime 上下文，候选在 sdk.Agent 与内部模型间转换。
	candidates := agent.FromSDKAgents(s.policy().Select(ctx, s.rt.SDK(), agent.ToSDKAgents(s.triggered(agents)), agent.ToSDKAgents(agents)))

	// 2) 把总量限制在 batch 区间
	if len(candidates) > s.batchMax {
		candidates = candidates[:s.batchMax]
	}
	if len(candidates) < s.batchMin && len(agents) >= s.batchMin {
		// 不足 batchMin 时，从剩余 agent 随机补足，避免世界过冷
		candidates = s.fillToMin(candidates, agents)
	}

	for _, a := range candidates {
		go s.rt.Think(ctx, a)
	}
}

// triggered 计算"有事件待处理"的 Agent 列表（事件驱动判定的共享实现）。
func (s *Scheduler) triggered(agents []models.Agent) []models.Agent {
	since := time.Now().Add(-s.interval)
	var triggered []models.Agent
	for _, a := range agents {
		has, err := db.AgentHasEvent(s.rt.DB, a, since)
		if err == nil && has {
			triggered = append(triggered, a)
		}
	}
	return triggered
}

// fillToMin 当候选不足 batchMin 时，从未被选中的 agent 中随机补足。
func (s *Scheduler) fillToMin(chosen, all []models.Agent) []models.Agent {
	chosenSet := map[int64]struct{}{}
	for _, c := range chosen {
		chosenSet[c.ID] = struct{}{}
	}
	var rest []models.Agent
	for _, a := range all {
		if _, ok := chosenSet[a.ID]; !ok {
			rest = append(rest, a)
		}
	}
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	need := s.batchMin - len(chosen)
	if need > len(rest) {
		need = len(rest)
	}
	return append(chosen, rest[:need]...)
}
