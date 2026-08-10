package goose

import "fmt"

// Perception 某个 Agent 的"世界视图"——信息隔离的关键。
//
// 关键原则（readme v0.2 §7）：Agent 看不到真实 GameState，只能看到 Perceive
// 基于它的身份/位置/阶段投影出的视图。因此：
//   - Agent 知道自己的身份（Team），但不知道别人的身份。
//   - Agent 知道自己在哪个房间、房间里还有谁，但不知道其他房间的人。
//   - Agent 知道"某房间有尸体"（被报告的），但不知道尸体是谁制造的。
//   - 会议阶段，Agent 知道所有存活者（都要开会），并能看到历史发言。
type Perception struct {
	// 自己的信息
	AgentID  int64
	Name     string
	Team     Team    // 自己的隐藏身份（鹅知道自己是鹅，鸭知道自己是鸭）
	IsAlive  bool
	Room     Room    // 自己所在房间
	TaskDone int     // 自己完成的任务数
	RoomIdx  int64   // 自己房间的索引（Move 动作用）

	// 世界阶段
	Phase Phase
	Round int

	// 自己所在房间的其他 Agent（同房的人——击杀/任务的时空范围）
	Roommates []AgentBrief

	// 全部存活者（会议阶段用，含跨房间）
	AliveList []AgentBrief

	// 房间（Move 可选目标，索引→房间名）
	Rooms []string

	// 已报告的尸体（谁被发现死了）——Agent 只知道"死了谁"，不知道凶手
	Bodies []BodyBrief

	// 尸体的现场人员快照（击杀发生时在房间里的人）。
	// 供 Agent 据此推断"现场可疑者"：对这些人提高怀疑。
	BodyScenes []BodyScene

	// 本 Agent 亲见"完成任务"的 Agent（同一房间、最近完成的）。
	// 供 Agent 据此降低对这些人物的怀疑（"我亲眼看他做任务"）。
	TaskWitnessed []int64

	// 最近世界事件（Agent 可能感知到的，如"有人在 X 移动了"）
	RecentEvents []string

	// 会议信息（Phase == PhaseMeeting 时非空）
	Meeting *MeetingBrief

	// 主观信念摘要（供 LLM Planner 决策上下文，v0.4）：
	// 本 Agent 对每个其他 Agent 的可疑度判断。这是 Agent 的内在状态，
	// 不是"谁最可疑"的结论——绝不包含 effectiveScore / MostSuspiciousOf 结果，
	// 以免规则系统替 LLM 做完决策。
	BeliefSummary []SuspectInfo

	// 关系摘要（供 LLM Planner 决策上下文，v0.4）：
	// 本 Agent 与每个其他 Agent 的好感度（-1 敌意 ~ +1 信任）。
	RelationshipSummary []RelInfo
}

// SuspectInfo 一个 Agent 的主观可疑度（Belief 摘要项）。
type SuspectInfo struct {
	AgentID int64
	Name    string
	Suspicion float64 // 0.0 ~ 1.0
}

// RelInfo 一个 Agent 与本 Agent 的关系（好感度摘要项）。
type RelInfo struct {
	AgentID   int64
	Name      string
	Goodwill  float64 // -1 敌意 ~ +1 信任
}

// AgentBrief 一个 Agent 的公开可见信息（无身份）。
type AgentBrief struct {
	ID    int64
	Name  string
	Alive bool
	Room  Room
}

// BodyBrief 一具尸体的公开可见信息（无凶手信息）。
type BodyBrief struct {
	AgentID int64 `json:"agentID"`
	Room    Room  `json:"room"`
}

// BodyScene 尸体的现场人员快照（信息隔离：Agent 只能看到"谁在现场"，仍不知凶手）。
type BodyScene struct {
	Victim int64
	Room   Room
	Scene  []int64 // 击杀发生时在场的 Agent ID
}

// MeetingBrief 会议视图。
type MeetingBrief struct {
	Reason    string
	VoteCount int
	// 谁已投票（可观察，但不能看到投给谁）
	Voters []string
	// 本 Agent 是否已在会议发言（v0.3：先发言再投票）
	HasSpoken bool
}

// BuildPerception 为某 Agent 构建视角投影（信息隔离的唯一切口）。
// 注意：这里只返回"Agent 该看到的"，绝不暴露 GameState 完整状态。
// 与 ApplyAction 并发执行，读必须加锁。
func (g *GameState) BuildPerception(id int64) *Perception {
	g.mu.Lock()
	defer g.mu.Unlock()

	me := g.Agents[id]
	if me == nil {
		return nil
	}
	p := &Perception{
		AgentID:  me.ID,
		Name:     me.Name,
		Team:     me.Team,
		IsAlive:  me.Alive,
		Room:     me.Room,
		TaskDone: me.TaskDone,
		RoomIdx:  roomIndex(me.Room),
		Phase:    g.Phase,
		Round:    g.Round,
	}
	// 房间列表（Move 目标）
	for _, r := range g.Rooms {
		p.Rooms = append(p.Rooms, string(r))
	}
	// 同房的人
	if me.Alive {
		for _, a := range g.Agents {
			if a.ID != me.ID && a.Alive && a.Room == me.Room {
				p.Roommates = append(p.Roommates, AgentBrief{ID: a.ID, Name: a.Name, Alive: true, Room: a.Room})
			}
		}
	}
	// 全部存活者
	for _, a := range g.Agents {
		if a.Alive {
			p.AliveList = append(p.AliveList, AgentBrief{ID: a.ID, Name: a.Name, Alive: true, Room: a.Room})
		}
	}
	// 已发现的尸体
	for _, b := range g.Bodies {
		if b.Found {
			p.Bodies = append(p.Bodies, BodyBrief{AgentID: b.AgentID, Room: b.Room})
			// 现场人员快照（M5.1 信息隔离增强）：只有发现者亲见现场，
			// 其他 Agent 只知道"有人死了"，看不到现场有谁。
			// 这让怀疑不再全局一致指向凶手，会议投票更分散，游戏更长。
			if b.ReporterID == id {
				p.BodyScenes = append(p.BodyScenes, BodyScene{Victim: b.AgentID, Room: b.Room, Scene: b.Scene})
			}
		}
	}
	// 亲见谁做任务：本 Agent 同房、最近完成任务的 Agent
	for _, t := range g.RecentTasks {
		if t.Room == me.Room {
			p.TaskWitnessed = append(p.TaskWitnessed, t.AgentID)
		}
	}
	// 最近事件
	p.RecentEvents = g.RecentEvents(8)
	// 会议视图
	if g.Phase == PhaseMeeting && g.Meeting != nil {
		p.Meeting = &MeetingBrief{
			Reason:    g.Meeting.Reason,
			VoteCount: len(g.Meeting.Votes),
			HasSpoken: g.Meeting.Spoken[id],
		}
		for voter := range g.Meeting.Votes {
			if a := g.Agents[voter]; a != nil {
				p.Meeting.Voters = append(p.Meeting.Voters, a.Name)
			}
		}
	}
	// 主观信念摘要（仅本 Agent 自己的 Belief，不含 effectiveScore 结论）。
	// me 在此处必然非 nil（前面已处理 nil 返回）。
	for tid, s := range me.Belief.Suspicions {
		if tid == me.ID {
			continue
		}
		p.BeliefSummary = append(p.BeliefSummary, SuspectInfo{
			AgentID:   tid,
			Name:      g.nameOf(tid),
			Suspicion: s,
		})
	}
	for tid, r := range me.Relationships {
		if tid == me.ID {
			continue
		}
		p.RelationshipSummary = append(p.RelationshipSummary, RelInfo{
			AgentID:  tid,
			Name:     g.nameOf(tid),
			Goodwill: r,
		})
	}
	return p
}

// nameOf 返回某 Agent 的名字（供感知摘要）。
func (g *GameState) nameOf(id int64) string {
	if a := g.Agents[id]; a != nil {
		return a.Name
	}
	return fmt.Sprintf("Agent%d", id)
}

// Describe 生成 Agent 可读的感知文本（喂给 Planner）。
// 这是信息隔离的最终呈现：Agent 只看到这里的内容，不接触 GameState。
func (p *Perception) Describe() string {
	if p == nil {
		return "你不在游戏中"
	}
	b := ""
	b += fmt.Sprintf("你（%s）的隐藏身份是【%s】。", p.Name, p.Team)
	if !p.IsAlive {
		b += "你已死亡。"
		return b
	}
	b += fmt.Sprintf("你当前在【%s】。", p.Room)
	if p.Phase == PhaseMeeting {
		b += fmt.Sprintf("现在开会中（%s），存活者：", p.Meeting.Reason)
		for _, a := range p.AliveList {
			b += fmt.Sprintf(" %s", a.Name)
		}
		b += "。你可以发言或投票。"
		return b
	}
	// 行动阶段
	if len(p.Roommates) > 0 {
		b += " 这个房间还有："
		for _, r := range p.Roommates {
			b += " " + r.Name
		}
		b += "。"
	} else {
		b += " 这个房间只有你一个人。"
	}
	if len(p.Bodies) > 0 {
		b += " 你知道以下人已死亡："
		for _, x := range p.Bodies {
			b += fmt.Sprintf(" %s", nameOf(x.AgentID))
		}
		b += "。"
	}
	if len(p.RecentEvents) > 0 {
		b += " 最近发生："
		for _, e := range p.RecentEvents {
			b += " [" + e + "]"
		}
		b += "。"
	}
	b += " 你可做的事：移动(move 到其他房间)、做任务(task)、击杀(kill)、报告尸体(report)。"
	return b
}

func nameOf(id int64) string { return fmt.Sprintf("Agent%d", id) }
