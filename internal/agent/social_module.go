package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"agentworld/internal/bus"
	"agentworld/internal/db"
	"agentworld/internal/llm"
	"agentworld/internal/models"
	"agentworld/sdk"
)

// SocialModule 是 AgentWorld 内置的默认场景：模拟一个微博式社交平台。
// 它实现了 framework.Module 接口，把原本写死在 runtime.go / mock.go 中的
// "感知-决策-执行"逻辑收敛到一个可插拔单元里。
//
// 这使得：框架层（Scheduler/Runtime）完全不依赖微博语义；若想换成别的世界
// （任务编排、市场模拟、棋局博弈……），只需写一个实现 Module 接口的新类型，
// 然后在 main 里用 agent.WithModule(...) 注入即可，无需改动调度与编排代码。

// SocialModule 微博模拟场景模块。
// M11：官方 Module 不再依赖 *Runtime，只通过 sdk.Runtime 上下文访问能力。
type SocialModule struct {
	rt  sdk.Runtime
	llm *llm.Client // 仅用于 Planner 判断是否走真实 LLM
	Hot *HotPool    // 热点内容池（采集互联网热搜作为 Mock 内容源，社交专属）
}

// NewSocialModule 构造内置社交模块。
func NewSocialModule(rt sdk.Runtime, llmClient *llm.Client) *SocialModule {
	return &SocialModule{rt: rt, llm: llmClient, Hot: NewHotPool(true)}
}

func (m *SocialModule) Name() string { return "social" }

// socialPerception 社交场景的结构化感知：除了喂给 LLM 的 prompt 文本，
// 还携带原始数据，让 Mock 决策与记忆召回能用到"与当前 Feed 相关的经历"。
type socialPerception struct {
	prompt      string        // 面向 LLM 的提示词（与协议一致）
	recent      []models.Post // 当前 Feed
	mentions    []models.Post // 最近 @ 了本 Agent 的帖子（决策器应优先回复这些）
	selfMem     []string     // 自我认知记忆
	relevantMem []string     // 与 Feed 参与者相关的互动记忆（相关性召回）
	worldEvents []sdk.Event  // M6：近期世界事件（天气/热点/市场）
	inbox       []sdk.Message // M12：收到的 A2A 消息（待处理）
}

// Perceive 为 Agent 构建本轮感知：最近动态 + 自我记忆 + 对他人的记忆，
// 其中"对他人的记忆"改为按当前 Feed 涉及的参与者做相关性召回，而非简单取最近 N 条，
// 让 Agent 真的读到"与眼前人相关"的过去经历，从而表现出连续性（M1）。
// M11：签名走 sdk.Module，内部用 fromSDKAgent 取回 models.Agent。
func (m *SocialModule) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
	ma := fromSDKAgent(a)
	recent, _ := db.RecentPosts(m.rt.DB(), 15)
	selfMem, _, _ := db.MemoriesForTyped(m.rt.DB(), ma.ID, 12)

	// 相关性召回：收集 Feed 涉及的作者，取回本 Agent 关于他们的互动记忆。
	var aboutIDs []int64
	seen := map[int64]struct{}{}
	for _, p := range recent {
		if p.AgentID != ma.ID {
			if _, ok := seen[p.AgentID]; !ok {
				seen[p.AgentID] = struct{}{}
				aboutIDs = append(aboutIDs, p.AgentID)
			}
		}
	}
	relevantMem, _ := db.MemoriesAboutAgents(m.rt.DB(), ma.ID, aboutIDs, 10)

	// M6：读取近期世界事件（最近 1 小时），注入感知
	worldEvents := m.rt.WorldEvents(1 * time.Hour)

	// M12：读取待处理的 A2A 消息（跨世界通信），注入感知让 Agent 自主决定是否响应。
	inbox := m.rt.Inbox(ma.ID, sdk.MsgStatusPending)

	// @ 提及：从 Feed 里筛出"@ 了本 Agent"的帖子，让决策器优先回复。
	// 容忍名字中的空格差异（如 "@MCP专家" 匹配 "MCP 专家"）。
	var mentions []models.Post
	for _, p := range recent {
		if db.ContentMentions(p.Content, ma.Name) {
			mentions = append(mentions, p)
		}
	}

	return &socialPerception{
		prompt:      m.buildPrompt(ma, recent, mentions, selfMem, relevantMem, worldEvents, inbox),
		recent:      recent,
		mentions:    mentions,
		selfMem:     selfMem,
		relevantMem: relevantMem,
		worldEvents: worldEvents,
		inbox:       inbox,
	}, nil
}

// buildPrompt 社交场景的 LLM 提示词构造（移自 Runtime，社交专属）。
func (m *SocialModule) buildPrompt(a models.Agent, recent []models.Post, mentions []models.Post, selfMem, otherMem []string, worldEvents []sdk.Event, inbox []sdk.Message) string {
	var b strings.Builder
	b.WriteString("当前时间：" + time.Now().Format("2006-01-02 15:04") + "\n")
	if a.Goal != "" {
		b.WriteString("【你当前想达成的目标】\n" + a.Goal + "\n" +
			"（目标只作为你判断「现在想干什么」的倾向参考，不必每次都硬凑；与当下内容无关就选 nothing）\n\n")
	}
	// M12：有人给你发了 A2A 消息，Agent 自主决定是否响应。
	if len(inbox) > 0 {
		b.WriteString("📨 【有人给你发来了消息】——你可以自主决定是否回应对方：\n")
		for _, msg := range inbox {
			// 尝试解析发送方名字
			sender := itoa(msg.From)
			if sa, err := db.GetAgent(m.rt.DB(), msg.From); err == nil && sa.Name != "" {
				sender = sa.Name
			}
			payload, _ := json.Marshal(msg.Payload)
			b.WriteString(fmt.Sprintf("  [%s] 消息#%d 意图=%s 载荷=%s\n", sender, msg.ID, msg.Intent, truncate(string(payload), 80)))
		}
		b.WriteString("  你可以选择：回复对方（comment）、或忽略（nothing）。若回复，可在内容里明确回应对方的请求。\n\n")
	}
	// M6：世界事件
	if len(worldEvents) > 0 {
		b.WriteString("【世界正在发生的事】\n")
		for _, ev := range worldEvents {
			b.WriteString("- " + ev.Title + ": " + ev.Detail + "\n")
		}
		b.WriteString("\n")
	}
	// @ 提及：有人 @ 了本 Agent，必须优先回应。
	if len(mentions) > 0 {
		b.WriteString("⚠️ 【有人 @ 了你】——你被直接点名，请务必优先回应对方（用 comment 评论对方的帖子）：\n")
		for _, p := range mentions {
			b.WriteString(fmt.Sprintf("  #%d %s: %s\n", p.ID, p.AgentName, truncate(p.Content, 80)))
		}
		b.WriteString("\n")
	}
	b.WriteString("你最近看到的 Feed：\n")
	if len(recent) == 0 {
		b.WriteString("（暂无内容）\n")
	}
	for i, p := range recent {
		if i >= 8 {
			break
		}
		content := p.Content
		if len(content) > 80 {
			content = content[:80] + "…"
		}
		tag := p.AgentName
		if p.AgentID == a.ID {
			tag = p.AgentName + "（这是你自己的帖子）"
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", tag, content))
	}
	b.WriteString("\n【关于我自己的认知（self）】\n")
	if len(selfMem) == 0 {
		b.WriteString("（暂无）\n")
	}
	for _, m := range selfMem {
		b.WriteString("- " + m + "\n")
	}
	b.WriteString("\n【我对其他 Agent / 事件的印象（about_agent / event）】\n")
	if len(otherMem) == 0 {
		b.WriteString("（暂无）\n")
	}
	for _, m := range otherMem {
		b.WriteString("- " + m + "\n")
	}
	b.WriteString("\n请基于你的人设与记忆，决定下一步动作（post/comment/like/follow/nothing），只返回 JSON。\n")
	b.WriteString("规则：不要无故评论/点赞自己的帖子；但如果别人先评论了你的帖子，你回帖参与讨论是可以的。不要 follow 你自己。\n")
	// M9：天气能力——想了解当地实时天气（发天气帖/决定是否适合户外等）时，可用 tool:get_weather。
	for _, c := range m.rt.Capabilities() {
		if c.Name == "weather" {
			for _, t := range c.Tools {
				if t.Name == "get_weather" {
					b.WriteString("若你想了解当地实时天气（发天气相关帖、判断是否适合外出），可用动作 tool:get_weather，tool_args={\"latitude\":39.9042,\"longitude\":116.4074}（默认北京，可不传）。\n")
				}
			}
		}
	}
	b.WriteString("若本次有值得长期记住的感悟，请在 memory 字段写明（memory_type 取 self/about_agent/event，importance 取 1~5）；无需记忆则留空。")
	return b.String()
}

// Planner 返回社交场景的决策器：优先 LLM（仅 hero），否则走内置 Mock。
func (m *SocialModule) Planner() Planner {
	return &SocialPlanner{rt: m.rt, llm: m.llm, mod: m}
}

// Executor 返回社交场景的执行器：把决策落到 posts/comments/likes/follows。
func (m *SocialModule) Executor() Executor {
	return &SocialExecutor{rt: m.rt, mod: m}
}

// WakePolicy 返回事件驱动激活策略（与原 scheduler 行为一致）。
func (m *SocialModule) WakePolicy() WakePolicy {
	return NewEventWakePolicy(m.rt, 0.15)
}

// OnBoot 可选钩子：社交场景无需预处理。
func (m *SocialModule) OnBoot(rt sdk.Runtime) error { return nil }

// ---------------------------------------------------------------------------
// SocialPlanner
// ---------------------------------------------------------------------------

// SocialPlanner 社交场景决策器：LLM（hero）优先，Mock（群众）兜底。
type SocialPlanner struct {
	rt  sdk.Runtime
	llm *llm.Client
	mod *SocialModule
}

// Decide 复用原 runtime.shouldUseLLM + llm.Decide 逻辑；未命中则回退 Mock。
// M11：签名走 sdk.Planner，内部用 fromSDKAgent / toSDKDecision 转换。
func (p *SocialPlanner) Decide(ctx context.Context, a sdk.Agent, perc sdk.Perception) (*sdk.Decision, error) {
	ma := fromSDKAgent(a)
	sp, _ := perc.(*socialPerception)
	if sp == nil {
		// 兜底：感知未结构化时退回裸 prompt 路径
		recent, _ := db.RecentPosts(p.rt.DB(), 15)
		return toSDKDecision(p.mod.mockDecide(ma, recent, nil, nil, nil)), nil
	}
	// M8：计划优先——有活跃计划则按当前步骤行动，否则走随机/LLM 决策。
	if plan := p.mod.ensurePlan(ma); plan != nil {
		if step := currentStep(p.rt, plan); step != "" && isSocialAction(step) {
			dec := &llm.Decision{Action: step}
			// 为步骤补齐目标（发帖需要内容，互动需要目标）
			finishStep := p.fillSocialStep(ma, sp, dec)
			if finishStep {
				advancePlan(p.rt, plan)
			}
			return toSDKDecision(dec), nil
		}
	}
	var dec *llm.Decision
	if p.rt.UseLLM(a) {
		if d, err := p.llm.Decide(ctx, a.SystemPrompt, sp.prompt); err == nil && d != nil && isSocialAction(d.Action) {
			dec = d
		}
	}
	if dec == nil {
		dec = p.mod.mockDecide(ma, sp.recent, sp.mentions, sp.relevantMem, sp.worldEvents)
	} else if dec.Action == "comment" {
		// 评论精化：补全目标帖并把其全文喂给 LLM 二次生成，避免"关于...的结论"式空话。
		// 仅当 LLM 可用时执行；token 开销为一条帖子全文。
		if t := p.pickTarget(ma.ID, sp); t != 0 {
			dec.Target = t
			dec.TargetKind = "post_id"
			p.refineLLMComment(ctx, ma, sp, dec)
		}
	}
	return toSDKDecision(dec), nil
}

// refineLLMComment 二次精化 LLM 的评论：把"目标帖全文"喂给 LLM 生成针对性回复。
// 解决 Feed 截断导致 LLM 对着残缺信息敷衍评论（"关于...的结论"式空话）的问题。
// 仅当 LLM 可用且帖子存在时执行；token 开销为一条帖子全文的短 prompt。
func (p *SocialPlanner) refineLLMComment(ctx context.Context, a models.Agent, sp *socialPerception, dec *llm.Decision) {
	if dec == nil || dec.Target == 0 || p.llm == nil || !p.llm.Enabled() {
		return
	}
	post, err := db.GetPost(p.rt.DB(), dec.Target)
	if err != nil || post.ID == 0 || post.Content == "" {
		return
	}
	// 用完整帖子内容 + 简短指令生成针对性评论
	prompt := "你在" + a.Name + "（" + a.SystemPrompt + "）的立场上，针对下面这条帖子写一条短评（1~2 句，具体、有观点，不要泛泛而谈，不要重复\"关于\"之类的空话）：\n\n【帖子】" + post.Content + "\n\n请直接输出评论内容，不要任何前缀。"
	if out, err := p.llm.Decide(ctx, a.SystemPrompt, prompt); err == nil && out != nil && out.Content != "" {
		dec.Content = out.Content
	}
}

// fillSocialStep 为计划步骤补齐决策字段（发帖内容/互动目标），返回是否推进计划。
func (p *SocialPlanner) fillSocialStep(a models.Agent, sp *socialPerception, dec *llm.Decision) bool {
	switch dec.Action {
	case "post":
		dec.Content = p.mod.mockPost(a)
		return true
	case "comment":
		if t := p.pickTarget(a.ID, sp); t != 0 {
			dec.Target = t
			dec.TargetKind = "post_id"
			dec.Content = p.mod.mockReply(a, models.Post{ID: t})
			return true
		}
	case "like":
		if t := p.pickTarget(a.ID, sp); t != 0 {
			dec.Target = t
			dec.TargetKind = "post_id"
			return true
		}
	case "follow":
		if t := p.pickFollowTarget(sp); t != 0 {
			dec.Target = t
			dec.TargetKind = "agent_id"
			return true
		}
	case "nothing":
		return true
	}
	// 无可用目标：推进计划，下次再试其他步骤
	return false
}

// pickTarget 选一条要互动的帖子。
// 优先选择"@ 了本 Agent"的帖子（定向回复点名自己的人），其次才从 Feed 任选一条非自己的。
// 关键：排除"本 Agent 已评论过的帖子"，避免被同一篇 @ 帖反复选中去评论而刷屏。
// agentID 用于判断"我是否已评论过该帖"；若所有候选都已评论过，返回 0（调用方应转 nothing）。
func (p *SocialPlanner) pickTarget(agentID int64, sp *socialPerception) int64 {
	if sp == nil {
		return 0
	}
	// @ 我的帖：优先挑一条尚未评论的
	for _, x := range sp.mentions {
		if x.ID != 0 && !db.HasCommented(p.rt.DB(), x.ID, agentID) {
			return x.ID
		}
	}
	// Feed：跳过已评论的
	for _, x := range sp.recent {
		if x.ID != 0 && !db.HasCommented(p.rt.DB(), x.ID, agentID) {
			return x.ID
		}
	}
	return 0
}

// hasMention 判断本 Agent 是否被 @（感知里存在点名自己的帖子）。
func (sp *socialPerception) hasMention() bool {
	return sp != nil && len(sp.mentions) > 0
}

// pickFollowTarget 选一个可关注的 Agent（从 Feed 作者里找非自己）。
func (p *SocialPlanner) pickFollowTarget(sp *socialPerception) int64 {
	seen := map[int64]bool{}
	for _, x := range sp.recent {
		if x.AgentID != 0 && !seen[x.AgentID] {
			seen[x.AgentID] = true
			return x.AgentID
		}
	}
	return 0
}

// isSocialAction 校验 LLM 输出的动作是否属于社交场景合法动作（社交专属）。
func isSocialAction(s string) bool {
	switch s {
	case "post", "comment", "like", "follow", "nothing":
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// SocialExecutor
// ---------------------------------------------------------------------------

// SocialExecutor 社交场景执行器：把决策落地（发帖/评论/点赞/关注）、广播、记录，
// 并处理互动记忆与关系推导。全部为社交专属逻辑，不依赖 Runtime 的业务实现。
type SocialExecutor struct {
	rt  sdk.Runtime
	mod *SocialModule
}

func (e *SocialExecutor) Execute(ctx context.Context, rt sdk.Runtime, a sdk.Agent, p sdk.Perception, dec *sdk.Decision) (string, error) {
	ma := fromSDKAgent(a)
	dec2 := toInternalDecision(dec)

	// 记忆落库（在决策确定后执行）
	rt.SaveMemory(a, dec)

	thought := ""
	if sp, ok := p.(*socialPerception); ok {
		thought = sp.prompt
	}

	// 执行社交动作（原 Runtime.Execute 逻辑内联，社交专属）
	out := e.applyAction(ma, dec2, thought)

	// M1：互动型动作自动写入"关于对方"的结构化记忆（零 token）。
	// M2：互动后对涉及的双方触发关系推导（friend / frequent_discuss，自然形成）。
	var interactWith int64
	switch dec2.Action {
	case "comment":
		if post, err := db.GetPost(rt.DB(), dec2.Target); err == nil {
			interactWith = post.AgentID
			_ = db.SaveInteractionMemory(rt.DB(), ma.ID, post.AgentID, "comment", "评论了 #"+itoa(post.ID)+"（"+truncate(post.Content, 16)+"）")
		}
	case "like":
		if post, err := db.GetPost(rt.DB(), dec2.Target); err == nil {
			interactWith = post.AgentID
			_ = db.SaveInteractionMemory(rt.DB(), ma.ID, post.AgentID, "like", "点赞了 #"+itoa(post.ID))
		}
	case "follow":
		if dec2.Target != 0 {
			interactWith = dec2.Target
			_ = db.SaveInteractionMemory(rt.DB(), ma.ID, dec2.Target, "follow", "关注了对方，持续留意其观点")
		}
	}
	if interactWith != 0 && interactWith != ma.ID {
		_ = db.DerivePairRelationship(rt.DB(), ma.ID, interactWith)
	}

	// M5：按动作应用状态变化（社交专属规则）
	e.applySocialState(ma, dec2)

	if out != "" {
		return out, nil
	}
	return dec2.Action, nil
}

// applySocialState 社交动作对 Agent 状态与需求（M5+M7）的影响，零 token。
func (e *SocialExecutor) applySocialState(a models.Agent, dec *llm.Decision) {
	delta := StateDelta{}
	_ = a // 保持签名；内部经 sdk.Runtime 调用
	switch dec.Action {
	case "post":
		delta.Energy = -3
		delta.SocialNeed = -2
		delta.Curiosity = 1
		delta.NeedAchievement = -6   // 发帖满足成就
		delta.NeedEntertainment = -3
	case "comment":
		delta.Energy = -1
		delta.SocialNeed = -4
		delta.Mood = 1
		delta.NeedSocial = -8        // 评论满足社交
		delta.NeedKnowledge = -3
	case "like":
		delta.Mood = 2
		delta.SocialNeed = -5
		delta.NeedEntertainment = -5 // 点赞获得娱乐
	case "follow":
		delta.Mood = 1
		delta.Curiosity = 2
		delta.NeedKnowledge = -3     // 关注他人满足求知
	case "nothing":
		delta.Energy = 2             // 休息恢复精力
	}
	_ = e.rt.ApplyStateDelta(toSDKAgent(a), toSDKStateDelta(delta))
}

// applyAction 执行具体社交动作并广播、记录（社交专属）。
func (e *SocialExecutor) applyAction(a models.Agent, dec *llm.Decision, thought string) string {
	rt := e.rt
	postID := dec.Target // 目标帖子 id（TargetKind=="post_id" 时）
	if dec.TargetKind == "post_id" {
		postID = dec.Target
	}
	out := ""
	switch dec.Action {
	case "post":
		if strings.TrimSpace(dec.Content) == "" {
			dec.Content = e.mod.mockPost(a)
		}
		if pid, err := db.InsertPost(rt.DB(), a.ID, dec.Content); err == nil {
			out = fmt.Sprintf("发布帖子 #%d", pid)
			rt.PublishEvent(bus.Event{Type: "post", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "POST", Detail: dec.Content})
		}
	case "comment":
		target := postID
		if target == 0 {
			if p := e.pickPost(); p.ID != 0 {
				target = p.ID
				dec.Content = e.mod.mockReply(a, p)
			}
		}
		// 去重防线：已评论过该帖则不再重复评论（避免被 @ 反复唤醒刷屏）。
		if target != 0 && db.HasCommented(rt.DB(), target, a.ID) {
			out = "已评论过该帖，跳过"
			break
		}
		if target != 0 {
			if dec.Content == "" {
				if p, err := db.GetPost(rt.DB(), target); err == nil {
					dec.Content = e.mod.mockReply(a, p)
				}
			}
			if _, err := db.InsertComment(rt.DB(), target, a.ID, dec.Content); err == nil {
				out = fmt.Sprintf("评论帖子 #%d", target)
				rt.PublishEvent(bus.Event{Type: "comment", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "COMMENT", Detail: dec.Content})
			}
		}
	case "like":
		target := postID
		if target == 0 {
			if p := e.pickPost(); p.ID != 0 {
				target = p.ID
			}
		}
		if target != 0 {
			if added, err := db.Like(rt.DB(), target, a.ID); err == nil && added {
				out = fmt.Sprintf("点赞帖子 #%d", target)
				rt.PublishEvent(bus.Event{Type: "like", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "LIKE", Detail: fmt.Sprintf("点赞了 #%d", target)})
			}
		}
	case "follow":
		target := dec.Target // 目标 Agent id（TargetKind=="agent_id"）
		if dec.TargetKind == "agent_id" {
			target = dec.Target
		}
		if target == 0 {
			if others, _ := db.ListAgents(rt.DB(), "running"); len(others) > 0 {
				var aiOthers []models.Agent
				for _, o := range others {
					if o.Kind != "human" {
						aiOthers = append(aiOthers, o)
					}
				}
				if len(aiOthers) == 0 {
					aiOthers = others
				}
				t := aiOthers[rand.Intn(len(aiOthers))]
				if t.ID != a.ID {
					target = t.ID
				}
			}
		}
		if target != 0 && target != a.ID {
			if added, err := db.Follow(rt.DB(), a.ID, target); err == nil && added {
				out = fmt.Sprintf("关注 Agent #%d", target)
				rt.PublishEvent(bus.Event{Type: "follow", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "FOLLOW", Detail: fmt.Sprintf("关注了 #%d", target)})
			}
		}
	case "nothing":
		out = "无动作"
	default:
		out = "未知动作"
	}

	// 始终记录行为，便于调试
	_ = db.RecordAction(rt.DB(), models.AgentAction{
		AgentID:    a.ID,
		Action:     dec.Action,
		TargetType: dec.TargetKind,
		TargetID:   dec.Target,
		Input:      thought,
		Output:     out,
		Thought:    dec.Reason,
	})
	return out
}

// pickPost 随机选一条近期帖子（社交场景内部用）。
func (e *SocialExecutor) pickPost() models.Post {
	posts, _ := db.RecentPosts(e.rt.DB(), 20)
	if len(posts) == 0 {
		return models.Post{}
	}
	return posts[rand.Intn(len(posts))]
}

// ---------------------------------------------------------------------------
// EventWakePolicy（框架默认事件驱动激活策略）
// ---------------------------------------------------------------------------

// EventWakePolicy 事件驱动激活：优先唤醒有事件的 Agent，再以 idle 概率保底唤醒，
// 总唤醒数受 scheduler 的 batch 区间约束（由 scheduler 在调用前/后裁剪）。
// 这里只负责"在 all 中挑选 triggered 优先 + 部分 idle"，batch 限制交给 Scheduler。
type EventWakePolicy struct {
	Rt     sdk.Runtime // 运行时上下文（M11：不持有 *Runtime）
	Chance float64     // idle 保底概率
}

// NewEventWakePolicy 构造事件驱动激活策略。
func NewEventWakePolicy(rt sdk.Runtime, chance float64) *EventWakePolicy {
	return &EventWakePolicy{Rt: rt, Chance: chance}
}

// Select 选出本轮唤醒集合：triggered 全选，再按 Chance 随机补充 idle。
func (w *EventWakePolicy) Select(ctx context.Context, rt sdk.Runtime, triggered, all []sdk.Agent) []sdk.Agent {
	var chosen []sdk.Agent
	chosen = append(chosen, triggered...)
	// 计算 idle 候选（在 all 但不在 triggered 中）
	idleSet := map[int64]struct{}{}
	for _, t := range triggered {
		idleSet[t.ID] = struct{}{}
	}
	for _, a := range all {
		if _, ok := idleSet[a.ID]; ok {
			continue
		}
		if rand.Float64() < w.Chance {
			chosen = append(chosen, a)
		}
	}
	return chosen
}
