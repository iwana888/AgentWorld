package goose

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

// Action 动作常量（Agent 的 Decision.Action 前缀，GooseGame Executor 解释）。
const (
	ActMove   = "move"   // 移动到目标房间
	ActTask   = "task"   // 完成任务
	ActKill   = "kill"   // 击杀（仅鸭）
	ActReport = "report" // 发现尸体并报告（触发会议）
	ActSpeak  = "speak"  // 会议发言
	ActVote   = "vote"   // 投票
)

// ActionSpec 描述一个动作的结果（供 Executor 返回给 Agent）。
type ActionSpec struct {
	OK      bool
	Message string
	GameOver bool   // 动作导致游戏结束
}

// ApplyAction 执行一个动作，修改 GameState。返回给 Agent 的结果文本。
// 这是规则的唯一执行入口：任何 Agent 的行为都必须经过这里校验。
// 注意：scheduler 并发调用多个 Agent 的 Think，这里必须加锁保证串行，
// 否则 GameState 并发读写会产生数据竞争（map 并发写 / nil 指针）。
func (g *GameState) ApplyAction(actorID int64, action string, target int64, content string) ActionSpec {
	g.mu.Lock()
	defer g.mu.Unlock()

	me := g.Agents[actorID]
	if me == nil {
		return ActionSpec{OK: false, Message: "你不在这个游戏里"}
	}
	if !me.Alive {
		return ActionSpec{OK: false, Message: "你已死亡，无法行动"}
	}
	if g.Phase == PhaseOver {
		return ActionSpec{OK: false, Message: "游戏已结束"}
	}

	res := func() ActionSpec {
		switch action {
		case ActMove:
			return g.doMove(me, target)
		case ActTask:
			return g.doTask(me)
		case ActKill:
			return g.doKill(me, target)
		case ActReport:
			return g.doReport(me)
		case ActSpeak:
			return g.doSpeak(me, target, content)
		case ActVote:
			return g.doVote(me, target)
		}
		return ActionSpec{OK: false, Message: "未知动作: " + action}
	}()
	// 记录最近一次动作（Agent Inspector 展示用）。
	if res.OK {
		me.LastAction = res.Message
	}

	// 每次行动后：尸体可能被路人发现（避免永远无人报告而卡死）。
	g.maybeDiscoverBody()
	// 会议阶段推进：防止有人一直发言不投票导致会议永不归票。
	g.tickMeeting()
	// 每次行动后都检查胜负（终局保证：即使 AI 决策很笨，游戏也必须有明确结束）。
	g.checkWinner()
	// 超时安全阀（游戏最大时长，防无限卡死）。
	g.checkTimeout()

	return res
}

// tickMeeting 会议阶段的推进器：若会议持续太久仍未集齐票数（有人一直发言不投），
// 则强制归票（未投票者视为弃权），避免会议卡死。
func (g *GameState) tickMeeting() {
	if g.Phase != PhaseMeeting || g.Meeting == nil || g.Meeting.Concluded {
		return
	}
	g.Meeting.Ticks++
	// 已投人数
	voted := len(g.Meeting.Votes)
	// 已投人数 + 未投人数（存活的）应等于存活数；若超过 存活数*2 轮还没集齐则强制归票
	if g.Meeting.Ticks > 2*(g.AliveCount()+1) && voted < g.AliveCount() {
		g.log("事件: 会议持续太久，强制归票（未投票者视为弃权）")
		g.concludeMeeting()
	}
}

// maybeDiscoverBody 若存在未发现的尸体，小概率触发"路人发现"并召开会议。
// 防止鸭子把人都杀光也没人报告尸体 → 会议永不触发 → 游戏卡死。
func (g *GameState) maybeDiscoverBody() {
	if g.Phase != PhaseAction {
		return
	}
	// 遍历未发现的尸体
	for i := range g.Bodies {
		b := &g.Bodies[i]
		if b.Found {
			continue
		}
		// 每轮有 30% 概率被"路过者"发现（模拟世界其他 Agent 看到）
		if rand.Intn(10) < 3 {
			b.Found = true
			victim := g.Agents[b.AgentID]
			name := fmt.Sprintf("Agent%d", b.AgentID)
			if victim != nil {
				name = victim.Name
			}
			g.log("事件: 有人在 %s 发现了 %s 的尸体！触发紧急会议", b.Room, name)
			g.startMeeting(fmt.Sprintf("有人在 %s 发现了 %s 的尸体", b.Room, name))
			return
		}
	}
}

func roomByIndex(i int64) Room {
	if i >= 0 && i < int64(len(allRooms)) {
		return allRooms[i]
	}
	return allRooms[0]
}

func roomIndex(r Room) int64 {
	for i, x := range allRooms {
		if x == r {
			return int64(i)
		}
	}
	return 0
}

// doMove 移动到目标房间（target = 房间索引）。
// M5.1：Agent 在 2D 空间内真实移动——更新坐标与朝向，SSE 推送真实 from/to 坐标，
// 前端据此做平滑插值动画（Agent 不再"瞬移到房间中心"，而是走过去）。
func (g *GameState) doMove(me *GameAgent, target int64) ActionSpec {
	if g.Phase != PhaseAction {
		return ActionSpec{OK: false, Message: "会议阶段不能移动"}
	}
	r := roomByIndex(target)
	if r == me.Room {
		return ActionSpec{OK: false, Message: "你已经在 " + string(r)}
	}
	fromX, fromY := me.X, me.Y
	fromRoom := me.Room
	// 目标房间内的落点：优先靠近通往本房间的入口，让移动更有"走过去"的感觉。
	tx, ty := targetPoint(r)
	// 朝向目标方向
	me.Facing = math.Atan2(ty-me.Y, tx-me.X)
	me.Room = r
	me.X, me.Y = tx, ty
	g.log("事件: %s 移动到了 %s", me.Name, r)
	g.publish("agent.moved", map[string]interface{}{
		"agent":    me.ID,
		"name":     me.Name,
		"fromRoom": fromRoom,
		"from":     map[string]float64{"x": fromX, "y": fromY},
		"toRoom":   string(r),
		"to":       map[string]float64{"x": tx, "y": ty},
		"facing":   me.Facing,
	})
	return ActionSpec{OK: true, Message: "你移动到了 " + string(r)}
}

// targetPoint 返回进入某房间时 Agent 的落点坐标。
// 尽量靠近入口（Door），若入口在房间内则取入口附近；否则取房间内随机点。
func targetPoint(r Room) (float64, float64) {
	if _, ok := RoomLayout[r]; !ok {
		return roomCenter(r)
	}
	// 落点直接在房间空间内随机取点，体现"走进来后散开"。
	return randomPointIn(r)
}

// doTask 在当前位置完成任务（鹅加分，鸭做任务会扣分但被允许——鸭需伪装）。
func (g *GameState) doTask(me *GameAgent) ActionSpec {
	if g.Phase != PhaseAction {
		return ActionSpec{OK: false, Message: "会议阶段不能做任务"}
	}
	// 检查当前房间是否有任务点
	found := false
	for _, t := range g.Tasks {
		if t.Room == me.Room {
			found = true
			break
		}
	}
	if !found {
		return ActionSpec{OK: false, Message: "这个房间没有任务点"}
	}
	// 鸭/中立做任务 = 伪装：日志与鹅完全一致（信息隔离，不暴露谁是真做任务），
	// 但只有鹅的 TaskDone 会计入"有效进度"（checkWinner 依赖它）。
	me.TaskDone++
	// 记录最近完成任务，供同房 Agent 感知"亲见谁做任务"（减疑）。
	g.RecentTasks = append(g.RecentTasks, TaskEvent{AgentID: me.ID, Room: me.Room})
	if len(g.RecentTasks) > 30 {
		g.RecentTasks = g.RecentTasks[len(g.RecentTasks)-30:]
	}
	g.log("事件: %s 在 %s 忙碌地完成了一个任务（进度 %d）", me.Name, me.Room, me.TaskDone)
	g.publish("task.completed", map[string]interface{}{
		"agent": me.ID, "name": me.Name, "room": me.Room, "progress": me.TaskDone,
	})
	return ActionSpec{OK: true, Message: fmt.Sprintf("你完成了任务（当前进度 %d）", me.TaskDone)}
}

// doKill 击杀（仅鸭）：需与目标同房。
func (g *GameState) doKill(me *GameAgent, target int64) ActionSpec {
	if g.Phase != PhaseAction {
		return ActionSpec{OK: false, Message: "会议阶段不能击杀"}
	}
	if me.Team != TeamDuck {
		return ActionSpec{OK: false, Message: "只有鸭子能击杀"}
	}
	// 击杀冷却
	if time.Since(me.KillCooldown) < 20*time.Second {
		return ActionSpec{OK: false, Message: "击杀技能还在冷却中"}
	}
	victim := g.Agents[target]
	if victim == nil || !victim.Alive {
		return ActionSpec{OK: false, Message: "目标不存在或已死亡"}
	}
	if victim.Room != me.Room {
		return ActionSpec{OK: false, Message: "你和目标不在同一个房间"}
	}
	// 执行击杀：产生尸体。记录"击杀时在场的人"（Scene 快照）——
	// 供其他 Agent 感知尸体后据此推断现场可疑者（基于事件时位置）。
	victim.Alive = false
	me.KillCooldown = time.Now()
	scene := []int64{}
	for _, a := range g.Agents {
		if a.ID != victim.ID && a.Alive && a.Room == victim.Room {
			scene = append(scene, a.ID)
		}
	}
	g.Bodies = append(g.Bodies, Body{AgentID: victim.ID, Room: victim.Room, Found: false, Scene: scene})
	g.log("事件: 有人在 %s 发现了异常，%s 突然倒地不起", victim.Room, victim.Name)
	g.publish("agent.killed", map[string]interface{}{
		"victim": victim.ID, "name": victim.Name, "room": victim.Room,
	})
	return ActionSpec{OK: true, Message: "你制造了一起事故"}
}

// doReport 发现并报告尸体（触发会议）。
func (g *GameState) doReport(me *GameAgent) ActionSpec {
	// 检查当前房间有无未发现的尸体
	for i := range g.Bodies {
		b := &g.Bodies[i]
		if !b.Found && b.Room == me.Room {
			b.Found = true
			victim := g.Agents[b.AgentID]
			name := fmt.Sprintf("Agent%d", b.AgentID)
			if victim != nil {
				name = victim.Name
			}
			g.log("事件: %s 报告了 %s 的尸体！触发紧急会议", me.Name, name)
			g.startMeeting(fmt.Sprintf("%s 在 %s 发现了 %s 的尸体", me.Name, me.Room, name))
			return ActionSpec{OK: true, Message: "你报告了尸体，触发会议"}
		}
	}
	return ActionSpec{OK: false, Message: "这里没有可报告的尸体"}
}

// doSpeak 会议发言（v0.3：发言可带指控目标）。
// target > 0 表示"我指控某人"：这条发言会被所有存活 Agent 听到，
// 使他们对被指控者 +0.10 可疑度（讨论影响信念，进而影响投票）。
// target == 0 表示一般性发言（无具体指控）。
func (g *GameState) doSpeak(me *GameAgent, target int64, content string) ActionSpec {
	if g.Phase != PhaseMeeting {
		return ActionSpec{OK: false, Message: "现在不是会议阶段"}
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ActionSpec{OK: false, Message: "发言内容为空"}
	}

	accused := g.Agents[target]
	if target > 0 && accused == nil {
		return ActionSpec{OK: false, Message: "指控目标不存在"}
	}

	// 发言内容：若带指控，构造指控式发言
	speech := content
	if target > 0 {
		speech = fmt.Sprintf("我怀疑 %s 很可疑", accused.Name)
		// 讨论影响信念：所有存活 Agent（除自己）听到指控，对被指控者 +0.10
		for _, a := range g.Agents {
			if !a.Alive || a.ID == me.ID {
				continue
			}
			a.Belief.AddSuspicion(target, 0.10)
		}
		// 关系（v0.3）：被指控者 B 对指控者 A 好感下降 -0.15。
		// "A 在会议上针对我，我对 A 的好感下降。"
		// 注意：doSpeak 在 ApplyAction 的锁内执行，直接操作被指控者的
		// Relationships（不能调 UpdateRelationship，它会重复加锁死锁）。
		if accused.Relationships == nil {
			accused.Relationships = map[int64]float64{}
		}
		v := accused.Relationships[me.ID] - 0.15
		if v < -1 {
			v = -1
		}
		accused.Relationships[me.ID] = v
	}
	// 标记该 Agent 已在会议上发言（先发言后投票）。
	if g.Meeting != nil {
		g.Meeting.Spoken[me.ID] = true
	}
	g.log("发言: %s：%s", me.Name, speech)
	g.publish("agent.spoke", map[string]interface{}{
		"agent": me.ID, "name": me.Name, "target": target, "text": speech,
	})
	return ActionSpec{OK: true, Message: "你在会议上发言了"}
}

// doVote 投票（target = 被投 AgentID，-1 = 弃权）。
func (g *GameState) doVote(me *GameAgent, target int64) ActionSpec {
	if g.Phase != PhaseMeeting {
		return ActionSpec{OK: false, Message: "现在不是会议阶段"}
	}
	if g.Meeting == nil {
		return ActionSpec{OK: false, Message: "没有进行中的会议"}
	}
	if target != -1 && (g.Agents[target] == nil || !g.Agents[target].Alive) {
		return ActionSpec{OK: false, Message: "投票目标不合法"}
	}
	if g.Meeting.Votes == nil {
		g.Meeting.Votes = map[int64]int64{}
	}
	g.Meeting.Votes[me.ID] = target
	g.log("投票: %s 投出了关键一票", me.Name)
	g.publish("vote.cast", map[string]interface{}{
		"agent": me.ID, "name": me.Name, "target": target,
	})
	// 检查是否所有人都投了 → 归票
	if len(g.Meeting.Votes) >= g.AliveCount() {
		g.concludeMeeting()
	}
	return ActionSpec{OK: true, Message: "你完成了投票"}
}

// startMeeting 开始一次会议。
func (g *GameState) startMeeting(reason string) {
	g.Phase = PhaseMeeting
	g.Meeting = &Meeting{
		Reason:  reason,
		Votes:   map[int64]int64{},
		Spoken:  map[int64]bool{},
		Round:   g.Round,
	}
	g.publish("meeting.started", map[string]interface{}{
		"reason": reason, "round": g.Round,
	})
}

// concludeMeeting 归票：得票最多的被淘汰；平票则无人淘汰（再给一轮行动）。
func (g *GameState) concludeMeeting() {
	if g.Meeting == nil || g.Meeting.Concluded {
		return
	}
	g.Meeting.Concluded = true
	counts := map[int64]int{}
	for _, v := range g.Meeting.Votes {
		if v != -1 {
			counts[v]++
		}
	}
	// 找出最高票
	maxVotes := 0
	var top []int64
	for id, n := range counts {
		if n > maxVotes {
			maxVotes = n
			top = []int64{id}
		} else if n == maxVotes {
			top = append(top, id)
		}
	}
	// 平票（多个最高且 >1）或全弃权 → 无人淘汰
	if len(top) == 1 && maxVotes > 0 {
		victim := g.Agents[top[0]]
		victim.Alive = false
		g.Meeting.Eliminated = victim.ID
		g.log("事件: 投票结束，%s 被公投淘汰（身份: %s）", victim.Name, victim.Team)
		g.publish("agent.eliminated", map[string]interface{}{
			"agent": victim.ID, "name": victim.Name, "team": victim.Team.String(), "by": "vote",
		})
		// 中立（Dodo）被投出去 = 它的胜利目标达成（"让自己被投票淘汰"）。
		// 必须立即判胜负，否则 Dodo 活着会陷入"鸭死光但 Dodo 未淘汰"的死锁。
		if victim.Team == TeamDodo {
			g.Phase = PhaseOver
			g.Winner = TeamDodo
			g.WinReason = victim.Name + " 达成目标：被投票淘汰"
			g.EndedBy = "win"
			g.Meeting = nil
			g.log("事件: 游戏结束！%s 胜利（%s）", TeamDodo, g.WinReason)
			g.publish("game.ended", map[string]interface{}{"winner": "dodo", "reason": g.WinReason, "endedBy": "win"})
			return
		}
	} else {
		g.Meeting.Eliminated = 0
		g.log("事件: 投票平票/无人得票，无人被淘汰")
	}
	g.Meeting = nil
	g.Phase = PhaseAction
	g.Round++
	// 每轮结束检查胜负
	g.checkWinner()
}

// checkWinner 判断游戏是否结束（严格按胜利条件，不依赖"鹅变聪明"）。
// 优先级：
//   ① 鹅全灭        → 鸭胜
//   ② 鸭全灭        → 鹅胜
//   ③ 鹅完成任务    → 鹅胜
//   ④ 中立达成目标  → 中立胜（Dodo 被投票淘汰，已在 concludeMeeting 处理）
// 注意：终局保证独立于 AI 行为——即使 Agent 决策很笨，任何合法状态最终
// 都会在 checkWinner 或 timeout 处结束，不会卡死。
func (g *GameState) checkWinner() {
	if g.Phase == PhaseOver {
		return
	}
	aliveDuck, aliveGoose, aliveDodo := 0, 0, 0
	totalTask := 0
	for _, a := range g.Agents {
		switch a.Team {
		case TeamDuck:
			if a.Alive {
				aliveDuck++
			}
		case TeamGoose:
			if a.Alive {
				aliveGoose++
			}
			totalTask += a.TaskDone // 只有鹅的进度算"有效任务"
		case TeamDodo:
			if a.Alive {
				aliveDodo++
			}
		}
	}
	// ① 鹅全灭 → 鸭胜
	if aliveGoose == 0 {
		g.Phase = PhaseOver
		g.Winner = TeamDuck
		g.WinReason = "鹅全部出局"
		g.EndedBy = "win"
		g.log("事件: 游戏结束！%s 胜利（%s）", TeamDuck, g.WinReason)
		g.publish("game.ended", map[string]interface{}{"winner": "duck", "reason": g.WinReason, "endedBy": "win"})
		return
	}
	// ② 鸭全灭 → 鹅胜
	if aliveDuck == 0 {
		g.Phase = PhaseOver
		g.Winner = TeamGoose
		g.WinReason = "鸭子被清除"
		g.EndedBy = "win"
		g.log("事件: 游戏结束！%s 胜利（%s）", TeamGoose, g.WinReason)
		g.publish("game.ended", map[string]interface{}{"winner": "goose", "reason": g.WinReason, "endedBy": "win"})
		return
	}
	// ③ 鹅完成任务 → 鹅胜（任务总量阈值：存活鹅数 × 15）
	if totalTask >= aliveGoose*15 {
		g.Phase = PhaseOver
		g.Winner = TeamGoose
		g.WinReason = "鹅完成了足够任务"
		g.EndedBy = "win"
		g.log("事件: 游戏结束！%s 胜利（%s）", TeamGoose, g.WinReason)
		g.publish("game.ended", map[string]interface{}{"winner": "goose", "reason": g.WinReason, "endedBy": "win"})
		return
	}
	// （④ Dodo 被投 → Dodo 胜，已在 concludeMeeting 判定）
}

// checkTimeout 超时安全阀：超过 MaxGameDuration 仍未结束则按当前可判定状态裁决。
// 这不是正常胜利条件，日志必须明确标记 timeout fallback。
// 裁决（MVP 简化）：
//   - 若鸭存活且鸭 ≥ 鹅 → 鸭胜
//   - 否则 → 鹅胜
func (g *GameState) checkTimeout() bool {
	if g.Phase == PhaseOver {
		return false
	}
	if time.Since(g.StartedAt) < MaxGameDuration {
		return false
	}
	aliveDuck, aliveGoose := 0, 0
	for _, a := range g.Agents {
		switch a.Team {
		case TeamDuck:
			if a.Alive {
				aliveDuck++
			}
		case TeamGoose:
			if a.Alive {
				aliveGoose++
			}
		}
	}
	g.Phase = PhaseOver
	g.EndedBy = "timeout"
	winnerStr := "goose"
	if aliveDuck > 0 && aliveDuck >= aliveGoose {
		g.Winner = TeamDuck
		g.WinReason = "timeout fallback：鸭存活且占优"
		winnerStr = "duck"
	} else {
		g.Winner = TeamGoose
		g.WinReason = "timeout fallback：鹅存活占优"
	}
	g.log("事件: 游戏结束！%s 胜利（%s）", g.Winner, g.WinReason)
	g.publish("game.ended", map[string]interface{}{"winner": winnerStr, "reason": g.WinReason, "endedBy": "timeout"})
	return true
}

// EndIfOver 返回游戏是否结束及结果（供 Executor 判断）。
func (g *GameState) EndIfOver() (over bool, winner Team, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Phase == PhaseOver {
		return true, g.Winner, g.WinReason
	}
	return false, 0, ""
}
