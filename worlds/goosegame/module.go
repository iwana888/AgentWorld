// Package goosegame —— AgentWorld 的第一个游戏世界（showcase demo）。
//
// 鸭鹅杀（Goose, Duck & Dodo）是一个社交推理游戏：
//   - 8 个 Agent 分配隐藏身份：6 鹅 / 1 鸭 / 1 中立。
//   - 行动阶段：鹅做任务找线索，鸭伺机击杀，中立隐藏自己。
//   - 发现尸体 → 触发会议：全员讨论、投票，被投票最多者淘汰。
//   - 胜负：鸭+中立全灭 = 鹅胜；鸭+中立 ≥ 鹅 = 鸭胜。
//
// 本模块严格遵循 SDK 契约（sdk.Module），只 import agentworld/sdk 与
// internal/llm（LLM 决策复用），不修改核心框架任何代码。
// 信息隔离：Agent 只能通过 Perceive 看到自己视角的投影，接触不到真实 GameState。
package goosegame

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"agentworld/internal/llm"
	"agentworld/sdk"
	"agentworld/worlds/goosegame/goose"
)

// GooseModule 鸭鹅杀世界模块。
type GooseModule struct {
	game *goose.GameState     // 真实世界状态（信息隔离：Agent 不可见全貌）
	llm  *llm.Client          // LLM 客户端（可选，无 key 走规则 Mock）
	obs  *goose.Observatory   // 观察台：收集游戏事件供前端消费（M5）
}

// New 创建鸭鹅杀模块。agentIDs 是参与游戏的 8 个 AgentWorld AgentID。
// llm 可空（空则所有 Agent 走规则决策）。
// 内部创建观察台（Observatory），收集游戏事件供 AI 社会观察台前端消费。
func New(agentIDs []int64, names []string, personalities []string, llmClient *llm.Client) *GooseModule {
	obs := goose.NewObservatory(goose.ObservOpts{MaxEvents: 1000})
	return &GooseModule{
		game: goose.NewGame(agentIDs, names, personalities, obs),
		llm:  llmClient,
		obs:  obs,
	}
}

// Game 返回游戏状态（仅供独立入口/观战读取，勿暴露给 Agent 决策）。
func (m *GooseModule) Game() *goose.GameState { return m.game }

// Observatory 返回观察台（前端 HTTP/SSE 消费事件流）。
func (m *GooseModule) Observatory() *goose.Observatory { return m.obs }

// Name 实现 sdk.Module。
func (m *GooseModule) Name() string { return "goosegame" }

// Perceive 实现 sdk.Module：为 Agent 构建信息隔离的视角。
func (m *GooseModule) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
	return m.game.BuildPerception(a.ID), nil
}

// Planner 实现 sdk.Module。
func (m *GooseModule) Planner() sdk.Planner { return &goosePlanner{mod: m} }

// Executor 实现 sdk.Module。
func (m *GooseModule) Executor() sdk.Executor { return &gooseExecutor{mod: m} }

// WakePolicy 实现 sdk.Module：行动阶段全部唤醒（8 人世界，每轮都该动）。
func (m *GooseModule) WakePolicy() sdk.WakePolicy { return AllWakePolicy{} }

// OnBoot 实现 sdk.Module。
func (m *GooseModule) OnBoot(rt sdk.Runtime) error {
	return nil
}

// ---------------------------------------------------------------------------
// Planner：把感知 + 隐藏身份转成动作。
// ---------------------------------------------------------------------------

type goosePlanner struct{ mod *GooseModule }

// Decide 生成一次决策。
// 流程（M0.1 架构升级）：Perception → UpdateBelief → Planner(Belief+Goal) → Decision。
// 有 LLM 且该 Agent UseLLM 时用 LLM；否则用 Belief 驱动的规则决策。
// 注意：这里不存在"因为角色是 X，所以投/做 Y"的行为硬编码——决策基于
// Agent 自己的 Belief（主观怀疑）与 Goal（身份目标）派生，角色只是 Identity。
func (p *goosePlanner) Decide(ctx context.Context, a sdk.Agent, perc sdk.Perception) (*sdk.Decision, error) {
	view, ok := perc.(*goose.Perception)
	if !ok || view == nil {
		return &sdk.Decision{Action: goose.ActTask}, nil
	}
	if !view.IsAlive {
		if view.Phase == goose.PhaseMeeting {
			return &sdk.Decision{Action: goose.ActSpeak, Content: "我虽然死了，但我想说……", Reason: "已死亡仍可表达"}, nil
		}
		return &sdk.Decision{Action: "idle", Reason: "已死亡，观望中"}, nil
	}

	// 先更新 Belief：基于本次感知，调整对每个 Agent 的主观怀疑。
	p.updateBelief(view)

	// LLM 决策（复用 llm.Client，与主程序一致：有 key 用 LLM）
	if a.UseLLM && p.mod.llm != nil && p.mod.llm.Enabled() {
		if d, err := p.llmDecide(ctx, a, view); err == nil {
			p.mod.game.SetLastDecision(view.AgentID, describeDecision(d))
			p.recordDecision(view, d)
			return d, nil
		}
	}
	// 规则决策（Belief 驱动）
	d := p.decideByBelief(view)
	p.mod.game.SetLastDecision(view.AgentID, describeDecision(d))
	p.recordDecision(view, d)
	return d, nil
}

// recordDecision 把一次决策的完整上下文记录为 DecisionRecord（M8）。
// 它同时更新 LastWhy 文本（供 Agent Brain 快速展示）。返回记录索引（Executor 回填用）。
func (p *goosePlanner) recordDecision(view *goose.Perception, d *sdk.Decision) int {
	rec := p.decisionContext(view, d)
	idx := p.mod.game.RecordDecision(rec)
	// 生成合并文本作为 LastWhy（保留给旧 UI / 快速查看）
	p.mod.game.SetLastWhy(view.AgentID, renderWhy(rec))
	return idx
}

// decisionContext 构造一次决策的结构化上下文（Goal/Perception/Memory/Relationship/Decision）。
func (p *goosePlanner) decisionContext(view *goose.Perception, d *sdk.Decision) goose.DecisionRecord {
	// 目标：由身份派生（与 Inspector 一致）
	goal := "找出并淘汰鸭子，完成任务保护大家"
	switch view.Team {
	case goose.TeamDuck:
		goal = "隐藏身份，消灭鹅群，避免被投出去"
	case goose.TeamDodo:
		goal = "让自己被投票淘汰（特殊目标）"
	}
	// 看到：当前房间 + 同房的人
	saw := "我在 " + string(view.Room)
	if len(view.Roommates) > 0 {
		names := make([]string, 0, len(view.Roommates))
		for _, r := range view.Roommates {
			names = append(names, r.Name)
		}
		saw += "，身边有 " + joinNames(names)
	} else {
		saw += "，独自一人"
	}
	// 记忆：最近的 1-2 条事件
	mem := ""
	if len(view.RecentEvents) > 0 {
		mem = view.RecentEvents[len(view.RecentEvents)-1]
	}
	// 关系：对"最关键对象"（最可疑者）的信任度摘要
	rel := ""
	if target, ok := p.mod.game.MostSuspiciousOf(view.AgentID, view.AliveList, 0); ok {
		targetName := fmt.Sprintf("Agent%d", target)
		if ta := p.mod.game.Agent(target); ta != nil {
			targetName = ta.Name
		}
		if a := p.mod.game.Agent(view.AgentID); a != nil {
			if gw, ok := a.Relationships[target]; ok {
				rel = fmt.Sprintf("对 %s 信任度 %+.2f（怀疑度 %.2f）", targetName, gw, a.Belief.Suspicions[target])
			} else {
				rel = fmt.Sprintf("对 %s 怀疑度 %.2f", targetName, a.Belief.Suspicions[target])
			}
		}
	}
	rec := goose.DecisionRecord{
		AgentID:      view.AgentID,
		Timestamp:    time.Now(),
		Goal:         goal,
		Perception:   saw,
		Memory:       mem,
		Relationship: rel,
		Decision:     describeDecision(d),
	}
	return rec
}

// renderWhy 把 DecisionRecord 渲染成合并文本（"为什么"多行）。
func renderWhy(rec goose.DecisionRecord) string {
	lines := []string{}
	if rec.Goal != "" {
		lines = append(lines, "目标："+rec.Goal)
	}
	if rec.Perception != "" {
		lines = append(lines, "看到："+rec.Perception)
	}
	if rec.Memory != "" {
		lines = append(lines, "记忆："+rec.Memory)
	}
	if rec.Relationship != "" {
		lines = append(lines, "关系："+rec.Relationship)
	}
	lines = append(lines, "因此：我决定"+rec.Decision)
	return strings.Join(lines, "\n")
}

// buildWhy 构造一次决策的完整依据（"为什么"），供 Agent Brain / Inspector 展示。
// 这是把 Agent 的"自主决策"变成肉眼可见的东西：目标 → 看到 → 记忆 → 性格 → 因此。
// 注意：这里展示的是行动理由/状态摘要，不是 LLM 的内部思维链。
func (p *goosePlanner) buildWhy(view *goose.Perception, d *sdk.Decision) string {
	// 目标：由身份派生（与 Inspector 一致）
	goal := "找出并淘汰鸭子，完成任务保护大家"
	switch view.Team {
	case goose.TeamDuck:
		goal = "隐藏身份，消灭鹅群，避免被投出去"
	case goose.TeamDodo:
		goal = "让自己被投票淘汰（特殊目标）"
	}
	// 看到：当前房间 + 同房的人
	saw := "我在 " + string(view.Room)
	if len(view.Roommates) > 0 {
		names := make([]string, 0, len(view.Roommates))
		for _, r := range view.Roommates {
			names = append(names, r.Name)
		}
		saw += "，身边有 " + joinNames(names)
	} else {
		saw += "，独自一人"
	}
	// 记忆：最近的 1-2 条事件
	mem := ""
	if len(view.RecentEvents) > 0 {
		mem = view.RecentEvents[len(view.RecentEvents)-1]
	}
	// 性格
	personality := ""
	if a := p.mod.game.Agent(view.AgentID); a != nil {
		personality = a.Personality
	}
	lines := []string{}
	if personality != "" {
		lines = append(lines, "性格："+personality)
	}
	lines = append(lines, "目标："+goal)
	lines = append(lines, "看到："+saw)
	if mem != "" {
		lines = append(lines, "记忆："+mem)
	}
	lines = append(lines, "因此：我决定"+describeDecision(d))
	return strings.Join(lines, "\n")
}

// joinNames 把名字列表连接成"甲、乙、丙"。
func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, "、")
}

// describeDecision 把决策翻译成人类可读的一句话（Agent Inspector 展示）。
func describeDecision(d *sdk.Decision) string {
	if d == nil {
		return "无决策"
	}
	base := map[string]string{
		"move": "移动", "task": "做任务", "kill": "击杀", "report": "报告尸体",
		"speak": "发言", "vote": "投票", "wait": "观望", "idle": "观望",
	}[d.Action]
	if base == "" {
		base = d.Action
	}
	if d.Reason != "" {
		return base + "（" + d.Reason + "）"
	}
	return base
}

// updateBelief 基于本次感知更新 Agent 的私有 Belief（对每个人的怀疑）。
// M0.1 启发式：
//   - 现场人员（尸体被发现时在房间里的人）→ +0.25
//   - 亲见某人完成任务 → -0.20（"我亲眼看他做任务，暂时更信他"）
//
// 这是 Agent 的主观状态：只更新自己的 Belief，不影响其他 Agent。
// 并发安全：通过 GameState.UpdateBelief 加锁，避免多 goroutine 并发写 Belief map。
func (p *goosePlanner) updateBelief(view *goose.Perception) {
	// 规则 A：尸体现场人员可疑
	for _, s := range view.BodyScenes {
		for _, id := range s.Scene {
			if id == view.AgentID {
				continue
			}
			p.mod.game.UpdateBelief(view.AgentID, id, 0.25)
		}
	}
	// 规则 B：亲见做任务减疑
	for _, id := range view.TaskWitnessed {
		if id == view.AgentID {
			continue
		}
		p.mod.game.UpdateBelief(view.AgentID, id, -0.20)
	}
}

// decideByBelief 基于 Belief + Goal 生成决策（无 LLM 时的规则决策）。
// 原则：角色不决定行为，Goal 决定优先级，Belief 决定"针对谁"。
//   - 投票：永远基于最高怀疑者（≥0.30 才投，否则弃票）——所有身份一致。
//   - 行动：按 Goal 分优先级（鹅守护/鸭猎杀/中立伪装），但都通过 Perception 决策。
func (p *goosePlanner) decideByBelief(view *goose.Perception) *sdk.Decision {
	// 会议阶段（v0.3）：先发言（指控最可疑者），再投票。
	// 发言会广播指控，影响其他 Agent 的 Belief；讨论后大家再投票。
	if view.Phase == goose.PhaseMeeting {
		// 还没发言 → 先发言（指控或表态）
		if view.Meeting == nil || !view.Meeting.HasSpoken {
			return p.speakInMeeting(view)
		}
		// 已发言 → 投票（基于讨论后的 Belief）
		return p.voteByBelief(view)
	}

	// 行动阶段：按各自 Goal 的优先级决策（角色 = 目标，而非行为规则）
	switch view.Team {
	case goose.TeamDuck:
		// Goal：隐藏自己 + 消灭鹅 → 找到落单者击杀，否则移动找人
		if len(view.Roommates) > 0 && rand.Intn(3) > 0 {
			victim := view.Roommates[rand.Intn(len(view.Roommates))].ID
			return &sdk.Decision{Action: goose.ActKill, Target: victim, Reason: "目标：消灭鹅群，寻找下手机会"}
		}
		return p.moveToRandom(view)
	case goose.TeamDodo:
		// Goal：让自己被投票淘汰 → 低调伪装做任务，等待会议
		if rand.Intn(2) == 0 {
			return &sdk.Decision{Action: goose.ActTask, Reason: "目标：隐藏自己，等待被投票"}
		}
		return p.moveToRandom(view)
	default: // TeamGoose
		// Goal：找出威胁 + 完成任务 → 看到尸体优先报告，否则做任务
		if len(view.Bodies) > 0 && rand.Intn(4) == 0 {
			return &sdk.Decision{Action: goose.ActReport, Reason: "目标：找出威胁，报告发现的尸体"}
		}
		if rand.Intn(3) > 0 {
			return &sdk.Decision{Action: goose.ActTask, Reason: "目标：完成任务，保护大家"}
		}
		return p.moveToRandom(view)
	}
}

// speakInMeeting 会议发言：若对某人达到怀疑阈值则指控他（广播给所有人），
// 否则做一般性表态。发言是"讨论影响信念"的载体——指控会让他人提高对被指控者的怀疑。
func (p *goosePlanner) speakInMeeting(view *goose.Perception) *sdk.Decision {
	const threshold = 0.30
	if target, ok := p.mod.game.MostSuspiciousOf(view.AgentID, view.AliveList, threshold); ok {
		return &sdk.Decision{
			Action:  goose.ActSpeak,
			Target:  target,
			Content: "我怀疑" + briefName(view, target),
			Reason:  fmt.Sprintf("我的观察指向 %s", briefName(view, target)),
		}
	}
	// 没有足够证据 → 一般表态（不指控任何人）
	return &sdk.Decision{
		Action:  goose.ActSpeak,
		Content: "我目前还没发现明确的可疑目标，大家怎么看？",
		Reason:  "暂时没有足够证据指控任何人",
	}
}

// voteByBelief 投票决策：投当前最可疑的存活者（≥0.30），否则弃票。
// 所有身份统一：没有足够证据就不投，不强迫"必须找个人投"。
func (p *goosePlanner) voteByBelief(view *goose.Perception) *sdk.Decision {
	const threshold = 0.30
	if target, ok := p.mod.game.MostSuspiciousOf(view.AgentID, view.AliveList, threshold); ok {
		return &sdk.Decision{
			Action: goose.ActVote,
			Target: target,
			Reason: fmt.Sprintf("根据我的观察，%s 是当前最可疑的人", briefName(view, target)),
		}
	}
	return &sdk.Decision{Action: goose.ActVote, Target: -1, Reason: "目前没有足够证据，弃票"}
}

func briefName(view *goose.Perception, id int64) string {
	for _, a := range view.AliveList {
		if a.ID == id {
			return a.Name
		}
	}
	return fmt.Sprintf("Agent%d", id)
}

// llmDecide 用 LLM 生成鸭子游戏决策（v0.4：LLM 自主 Planner）。
//
// 输入：Identity + Goal + Perception + Memory + Belief + Relationship。
// 关键：LLM 只拿到"材料"（我看到什么、我怀疑谁、我和谁关系如何），
// 不拿到 effectiveScore / MostSuspiciousOf 的结论——由 LLM 自己做判断，
// 规则系统不替它决策。它可能判断错误、可能偏见、可能撒谎，这正是涌现的来源。
//
// 输出：结构化 Decision（action/target/content）。World Executor 负责合法性校验，
// LLM 永远不能绕过 World Rules（例如 Goose 想 kill 会被 Reject）。
func (p *goosePlanner) llmDecide(ctx context.Context, a sdk.Agent, view *goose.Perception) (*sdk.Decision, error) {
	system := "你是这个世界中的一个 Agent。你不知道其他 Agent 的真实身份。" +
		"你的记忆可能不完整，你的判断可能错误。" +
		"你可以相信别人，也可以怀疑别人。你可以撒谎，可以隐瞒，可以报复，可以结盟。" +
		"你应该根据自己的目标、经历、记忆、信念和关系做决定，而不是做一个完美理性的分析机器。" +
		"每次只做一件事。输出严格 JSON（不要多余文字）：{\"action\":\"move|task|kill|report|speak|vote|wait\",\"target\":数字或0,\"content\":\"发言或理由\",\"reason\":\"你的理由\"}。" +
		"其中 speak 的 target 是你要指控的 Agent 编号（无指控则 target 为 0），content 是你的发言；" +
		"vote 的 target 是你投票的 Agent 编号（弃票则 target 为 -1）；" +
		"move 的 target 是房间编号(0=Cafeteria,1=Engine,2=Storage,3=Laboratory,4=Security,5=Corridor)；kill 只在你是鸭子时可能成功。"

	user := p.buildLLMContext(view)
	dec, err := p.mod.llm.Decide(ctx, system, user)
	if err != nil {
		return nil, err
	}
	return &sdk.Decision{
		Action:  dec.Action,
		Target:  dec.Target,
		Content: dec.Content,
		Reason:  dec.Reason,
	}, nil
}

// buildLLMContext 构造 LLM 的决策上下文（身份/目标/观察/记忆/信念/关系）。
// 注意：不含 effectiveScore 或"谁最可疑"的结论——那是规则替 LLM 做决策。
func (p *goosePlanner) buildLLMContext(view *goose.Perception) string {
	b := ""
	// 身份与目标（Goal 由身份派生）
	b += fmt.Sprintf("你的身份：%s\n", view.Team)
	switch view.Team {
	case goose.TeamGoose:
		b += "你的目标：找出并淘汰 Duck，完成任务保护大家。\n"
	case goose.TeamDuck:
		b += "你的目标：隐藏自己的身份，消灭 Goose，避免被投出去。\n"
	case goose.TeamDodo:
		b += "你的目标：让自己被投票淘汰（这是你的特殊目标）。\n"
	}
	if !view.IsAlive {
		b += "你已死亡，只能观望。\n"
		return b
	}
	b += fmt.Sprintf("你的位置：%s\n", view.Room)

	// 看到的世界
	b += "你看到：\n"
	if len(view.Roommates) > 0 {
		b += "  当前房间还有：" + joinBriefs(view.Roommates) + "\n"
	} else {
		b += "  当前房间只有你一个人。\n"
	}
	if len(view.Bodies) > 0 {
		b += "  你知道以下人已死亡：" + joinBodyBriefs(view.Bodies) + "\n"
	}
	// 最近事件（记忆）
	if len(view.RecentEvents) > 0 {
		b += "最近发生的事（你的记忆）：\n"
		for _, e := range view.RecentEvents {
			b += "  - " + e + "\n"
		}
	}

	// 会议信息
	if view.Meeting != nil {
		b += fmt.Sprintf("正在开会（原因：%s）。存活者：%s\n", view.Meeting.Reason, joinBriefs(view.AliveList))
	}

	// 主观信念（Belief）
	b += "你的主观判断（对每人的可疑度，0~1）：\n"
	if len(view.BeliefSummary) == 0 {
		b += "  （你目前对任何人都还没有明确怀疑）\n"
	}
	for _, s := range view.BeliefSummary {
		b += fmt.Sprintf("  %s：可疑度 %.2f\n", s.Name, s.Suspicion)
	}

	// 关系（Relationship）
	b += "你与其他人的关系（好感度，负=敌意，正=信任）：\n"
	if len(view.RelationshipSummary) == 0 {
		b += "  （你与他们还没有明显的私人关系）\n"
	}
	for _, r := range view.RelationshipSummary {
		b += fmt.Sprintf("  %s：%.2f\n", r.Name, r.Goodwill)
	}

	b += "\n你现在想做什么？"
	return b
}

func joinBriefs(items []goose.AgentBrief) string {
	parts := make([]string, 0, len(items))
	for _, a := range items {
		parts = append(parts, fmt.Sprintf("%s(在%s)", a.Name, a.Room))
	}
	return strings.Join(parts, "、")
}

func joinBodyBriefs(items []goose.BodyBrief) string {
	parts := make([]string, 0, len(items))
	for _, b := range items {
		parts = append(parts, fmt.Sprintf("Agent%d死在了%s", b.AgentID, b.Room))
	}
	return strings.Join(parts, "、")
}

func (p *goosePlanner) moveToRandom(view *goose.Perception) *sdk.Decision {
	if len(view.Rooms) <= 1 {
		return &sdk.Decision{Action: goose.ActTask, Reason: "无其他房间可去"}
	}
	target := rand.Int63n(int64(len(view.Rooms)))
	if target == view.RoomIdx {
		target = (target + 1) % int64(len(view.Rooms))
	}
	return &sdk.Decision{Action: goose.ActMove, Target: target, Reason: "换个房间看看"}
}

// ---------------------------------------------------------------------------
// Executor：把决策落到真实世界（通过规则引擎校验）。
// ---------------------------------------------------------------------------

type gooseExecutor struct{ mod *GooseModule }

// Execute 实现 sdk.Executor。
func (e *gooseExecutor) Execute(ctx context.Context, rt sdk.Runtime, a sdk.Agent, perc sdk.Perception, dec *sdk.Decision) (string, error) {
	if dec == nil {
		return "无决策", nil
	}
	// 已死亡不行动
	view, _ := perc.(*goose.Perception)
	if view != nil && !view.IsAlive && dec.Action != goose.ActSpeak {
		return "已死亡，无法行动", nil
	}
	// 交给规则引擎执行（Approve/Reject 由引擎判定）
	res := e.mod.game.ApplyAction(a.ID, dec.Action, dec.Target, dec.Content)
	// 回填 DecisionRecord 的 Action / Outcome（M8：让"为什么"闭环——决策 + 结果）
	e.mod.game.SetDecisionOutcome(a.ID, describeDecision(dec), res.Message)
	// 检查游戏是否结束
	if over, winner, reason := e.mod.game.EndIfOver(); over {
		return fmt.Sprintf("%s → 游戏结束！%s 胜利（%s）", res.Message, winner, reason), nil
	}
	return res.Message, nil
}

// AllWakePolicy 全部唤醒策略：每轮唤醒所有 Agent（游戏世界节奏，8 人全参与）。
type AllWakePolicy struct{}

// Select 实现 sdk.WakePolicy：返回全部 Agent。
func (AllWakePolicy) Select(ctx context.Context, rt sdk.Runtime, triggered, all []sdk.Agent) []sdk.Agent {
	return all
}
