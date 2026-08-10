// Package goose —— 鸭鹅杀（AgentWorld 第一个游戏世界）。
//
// 设计哲学（对应 docs：Runtime 不知道世界是什么，World 只提供规则和环境）：
//   - GameState 保存"真实世界状态"，由 Module 持有。
//   - Agent 不能直接读 GameState，只能通过 Perceive 拿到自己视角的投影（信息隔离）。
//   - Agent 是"决策者"，GameState 是"规则执行者"：Agent 可以想干任何事，
//     但合法性由 ApplyAction 校验（距离/身份/存活/阶段）。
//   - 游戏推进是被动的（方案 X）：Agent 行动后检查游戏条件（尸体→会议→投票→归票）。
package goose

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// 阵营身份（隐藏身份，只存在于 GameState，Agent 只知道自己的）。
type Team int

const (
	TeamGoose Team = iota // 鹅：完成任务或淘汰鸭子
	TeamDuck              // 鸭：杀死鹅
	TeamDodo              // 中立：让自己被投票淘汰
)

func (t Team) String() string {
	switch t {
	case TeamGoose:
		return "Goose"
	case TeamDuck:
		return "Duck"
	case TeamDodo:
		return "Dodo"
	}
	return "Unknown"
}

// 游戏阶段。
type Phase int

const (
	PhaseAction  Phase = iota // 行动阶段：Agent 自由活动
	PhaseMeeting              // 会议阶段：讨论 + 投票
	PhaseOver                 // 游戏结束
)

func (p Phase) String() string {
	switch p {
	case PhaseAction:
		return "action"
	case PhaseMeeting:
		return "meeting"
	case PhaseOver:
		return "over"
	}
	return "unknown"
}

// 房间（M5 v0.1：6 个区域，readme v0.2 §4）。
type Room string

const (
	RoomCafeteria   Room = "Cafeteria"
	RoomEngine      Room = "Engine"
	RoomStorage     Room = "Storage"
	RoomLaboratory  Room = "Laboratory"
	RoomSecurity    Room = "Security"
	RoomCorridor    Room = "Corridor"
)

var allRooms = []Room{RoomCafeteria, RoomEngine, RoomStorage, RoomLaboratory, RoomSecurity, RoomCorridor}

// RoomRect 一个房间在 2D 地图上的矩形空间（M5.1：房间是"空间"，不是节点）。
// MinX/MinY/MaxX/MaxY 定义房间可站立范围；Agent 拥有真实坐标，在这个范围内移动。
type RoomRect struct {
	MinX, MinY, MaxX, MaxY float64
	// Door 通往相邻房间的出口（Agent 从一个房间走向另一个时经过）。
	DoorX, DoorY float64
}

// RoomLayout 全地图布局：画布 720x640，垂直中轴（Cafeteria 中心），房间为空间。
var RoomLayout = map[Room]RoomRect{
	RoomCafeteria:  {MinX: 260, MinY: 200, MaxX: 460, MaxY: 330, DoorX: 360, DoorY: 330},
	RoomEngine:     {MinX: 260, MinY: 490, MaxX: 460, MaxY: 610, DoorX: 360, DoorY: 490},
	RoomStorage:    {MinX: 290, MinY: 40, MaxX: 410, MaxY: 170, DoorX: 360, DoorY: 170},
	RoomLaboratory: {MinX: 470, MinY: 200, MaxX: 660, MaxY: 330, DoorX: 470, DoorY: 265},
	RoomSecurity:   {MinX: 60, MinY: 200, MaxX: 250, MaxY: 330, DoorX: 250, DoorY: 265},
	RoomCorridor:   {MinX: 260, MinY: 350, MaxX: 460, MaxY: 470, DoorX: 360, DoorY: 350},
}

// roomCenter 返回房间中心坐标（Agent 出生/初始位置）。
func roomCenter(r Room) (float64, float64) {
	if rect, ok := RoomLayout[r]; ok {
		return (rect.MinX + rect.MaxX) / 2, (rect.MinY + rect.MaxY) / 2
	}
	return 360, 265 // fallback Cafeteria
}

// randomPointIn 返回房间内一个随机坐标（偏离中心，避免所有 Agent 叠在中心）。
func randomPointIn(r Room) (float64, float64) {
	rect := RoomLayout[r]
	w := rect.MaxX - rect.MinX
	h := rect.MaxY - rect.MinY
	// 留出边缘 padding，坐标均匀散落在房间空间内
	x := rect.MinX + 20 + rand.Float64()*(w-40)
	y := rect.MinY + 20 + rand.Float64()*(h-40)
	return x, y
}

// Belief 一个 Agent 的主观信念（对每个其他 Agent 的可疑度）。
// 这是 Agent 的私有状态：只能由该 Agent 自己的感知更新，其他 Agent 和
// 公共规则逻辑不得读取它（Perceive 不暴露）。不同 Agent 对同一人的
// 怀疑可以完全不同。
type Belief struct {
	Suspicions map[int64]float64 // targetID -> 可疑度 (0.0 ~ 1.0)
}

// AddSuspicion 调整对某 Agent 的可疑度，并 clamp 到 [0,1]。
func (b *Belief) AddSuspicion(id int64, delta float64) {
	if b.Suspicions == nil {
		b.Suspicions = map[int64]float64{}
	}
	v := b.Suspicions[id] + delta
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	b.Suspicions[id] = v
}

// UpdateBelief 线程安全地更新某 Agent 对另一个 Agent 的可疑度。
// 这是 module.go（Planner）在并发 Think 中更新 Belief 的唯一入口——
// 必须加锁，否则多个 goroutine 同时写同一个 Belief map 会触发
// "concurrent map read and map write" fatal error。
func (g *GameState) UpdateBelief(observerID, targetID int64, delta float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a := g.Agents[observerID]; a != nil {
		a.Belief.AddSuspicion(targetID, delta)
	}
}

// MostSuspiciousOf 线程安全地返回某 Agent 眼中最可疑的存活者。
// 决策模型：effectiveScore = Suspicion - Goodwill*0.05。
//   - 关系（Goodwill）是偏置，不是证据——它只在决策瞬间微调可疑度，
//     绝不修改 Belief 本身（否则无法区分"我觉得他是鸭"和"我讨厌他"）。
//   - 弃票阈值仍由调用方（threshold）决定，关系不会改变"证据是否足够"。
func (g *GameState) MostSuspiciousOf(agentID int64, alive []AgentBrief, threshold float64) (int64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.Agents[agentID]
	if a == nil {
		return 0, false
	}
	best := int64(0)
	bestScore := 0.0
	found := false
	for id, suspicion := range a.Belief.Suspicions {
		if id == agentID || !isAlive(alive, id) {
			continue
		}
		// 关系偏置：-Goodwill*0.05（敌意 Goodwill<0 提高可疑，信任>0 降低）
		goodwill := a.Relationships[id]
		effective := suspicion - goodwill*0.05
		if effective >= threshold && effective > bestScore {
			best = id
			bestScore = effective
			found = true
		}
	}
	return best, found
}

// UpdateRelationship 线程安全地调整某 Agent 对另一个 Agent 的好感度，clamp 到 [-1,1]。
// 会议互动驱动（v0.3）：明确指控 → -0.15；明确支持 → +0.05。
func (g *GameState) UpdateRelationship(observerID, targetID int64, delta float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a := g.Agents[observerID]
	if a == nil {
		return
	}
	if a.Relationships == nil {
		a.Relationships = map[int64]float64{}
	}
	v := a.Relationships[targetID] + delta
	if v < -1 {
		v = -1
	}
	if v > 1 {
		v = 1
	}
	a.Relationships[targetID] = v
}

// MostSuspicious 返回当前最可疑的存活者及其可疑度。
// 若没有足够高的怀疑目标（< threshold），返回 (0, false) 表示应该弃票。
// 排除自己和已死亡者。
func (b *Belief) MostSuspicious(alive []AgentBrief, selfID int64, threshold float64) (int64, bool) {
	best := int64(0)
	bestScore := 0.0
	found := false
	for id, score := range b.Suspicions {
		if id == selfID {
			continue
		}
		if !isAlive(alive, id) {
			continue
		}
		if score >= threshold && score > bestScore {
			best = id
			bestScore = score
			found = true
		}
	}
	return best, found
}

func isAlive(alive []AgentBrief, id int64) bool {
	for _, a := range alive {
		if a.ID == id {
			return a.Alive
		}
	}
	return false
}

// GameAgent 一个参与游戏的 Agent 的真实状态（GameState 视角）。
type GameAgent struct {
	ID           int64              // 对应 AgentWorld 的 AgentID
	Name         string             // 名字
	Team         Team               // 隐藏身份（鹅/鸭/中立）
	Alive        bool               // 是否存活
	Room         Room               // 当前位置
	TaskDone     int                // 已完成任务数（鹅用）
	KillCooldown time.Time          // 鸭子击杀冷却（避免连续杀）
	Belief       Belief             // 本 Agent 的私有信念（可疑度，基于观察事实）
	Relationships map[int64]float64 // 本 Agent 对其他 Agent 的好感度（-1 敌意 ~ +1 信任）
	LastDecision string             // 最近一次决策的说明（Agent Inspector 展示）
	LastAction   string             // 最近一次执行的动作文本
	// M5.1 2D 空间：Agent 拥有真实坐标与朝向，在房间"空间"内自由移动。
	X, Y  float64 // 当前位置（地图坐标）
	Facing float64 // 朝向角度（弧度，0=右，π/2=下，用于前端角色朝向动画）
}

// Task 一个房间里的任务点。
type Task struct {
	Room Room
	Name string
}

// Body 一具尸体（等待被发现）。
type Body struct {
	AgentID int64
	Room    Room
	Found   bool
	// Scene 击杀发生时在房间里的 Agent（现场人员快照，供 Agent 推断可疑者）。
	// 基于"事件时位置"而非"发现时位置"：5 秒后才进房间的人不算现场。
	Scene []int64
}

// Meeting 一次会议（尸体发现或触发后进入）。
type Meeting struct {
	Reason   string          // 会议原因（"发现尸体"/"紧急会议"）
	Speaker  int64           // 当前发言的 AgentID
	Votes    map[int64]int64 // voter -> 投给的 AgentID（-1=弃权）
	Spoken   map[int64]bool  // 谁已在会议上发过言（v0.3：先发言再投票）
	Concluded bool           // 是否已归票
	Eliminated int64         // 被淘汰的 AgentID（0=未定/平票）
	Round     int
	Ticks     int             // 会议已推进的轮次（tickMeeting 用，防卡死）
}

// TaskEvent 一次任务完成（供 Agent 感知"亲见某人做任务"）。
type TaskEvent struct {
	AgentID int64
	Room    Room
}

// GameState 真实世界状态（信息隔离：Agent 只能看 Perceive 给的投影）。
// 注意：多个 Agent 的 Think 由 scheduler 并发执行，所有对 GameState 的
// 读写必须持锁，否则数据竞争（map 并发写 / nil 指针）。
type GameState struct {
	mu         sync.Mutex
	obs        *Observatory // M5：观察台事件总线（可空，为空则不发布）
	Phase      Phase
	Agents     map[int64]*GameAgent
	Rooms      []Room
	Tasks      []Task
	Bodies     []Body
	Meeting    *Meeting
	Round      int
	StartedAt  time.Time
	Winner     Team
	WinReason  string
	EndedBy    string // 结束原因：win / timeout / stalemate / draw
	// 记录最近事件，供 Perceive 展示（世界只有一个，Agent 各看各的）
	EventLog   []string
	// 最近完成的任务（供 Agent 感知"亲见谁做任务"，减疑）
	RecentTasks []TaskEvent
}

// 游戏终局保障常量。
const (
	// MaxGameDuration 一局游戏的最大时长：超时后按当前可判定状态裁决（系统安全阀，
	// 不是正常胜利条件，日志必须明确标记 timeout）。
	MaxGameDuration = 10 * time.Minute
)

// NewGame 初始化一局游戏：分配 8 个 Agent 的身份（6 鹅 / 1 鸭 / 1 中立）。
// agentIDs 是参与游戏的 AgentWorld AgentID 列表（需 8 个）。
func NewGame(agentIDs []int64, names []string, obs *Observatory) *GameState {
	g := &GameState{
		Phase:     PhaseAction,
		Agents:    map[int64]*GameAgent{},
		Rooms:     allRooms,
		Round:     1,
		StartedAt: time.Now(),
		obs:       obs,
	}
	// 身份分配：6 鹅 / 1 鸭 / 1 中立，随机洗牌
	teams := make([]Team, len(agentIDs))
	for i := range teams {
		teams[i] = TeamGoose
	}
	teams[len(agentIDs)-2] = TeamDuck
	teams[len(agentIDs)-1] = TeamDodo
	rand.Shuffle(len(teams), func(i, j int) { teams[i], teams[j] = teams[j], teams[i] })

	for i, id := range agentIDs {
		name := fmt.Sprintf("Agent%d", id)
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		// 出生在随机房间，拥有房间内的真实坐标（M5.1 2D 空间）
		room := allRooms[rand.Intn(len(allRooms))]
		x, y := randomPointIn(room)
		g.Agents[id] = &GameAgent{
			ID:    id,
			Name:  name,
			Team:  teams[i],
			Alive: true,
			Room:  room,
			X:     x,
			Y:     y,
			Facing: rand.Float64() * 2 * math.Pi,
		}
	}
	// 每个房间撒一些任务点（鹅完成它们）
	for _, r := range allRooms {
		g.Tasks = append(g.Tasks,
			Task{Room: r, Name: "FixWire"},
			Task{Room: r, Name: "Calibrate"},
		)
	}
	return g
}

// Agent 返回某 Agent 的游戏状态；不存在或未参与返回 nil。
func (g *GameState) Agent(id int64) *GameAgent { return g.Agents[id] }

// SetLastDecision 记录某 Agent 最近一次决策（线程安全，Agent Inspector 展示用）。
func (g *GameState) SetLastDecision(id int64, text string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if a := g.Agents[id]; a != nil {
		a.LastDecision = text
	}
}

// AliveCount 存活 Agent 数。
func (g *GameState) AliveCount() int {
	n := 0
	for _, a := range g.Agents {
		if a.Alive {
			n++
		}
	}
	return n
}

// AliveIDs 存活 Agent 的 ID 列表。
func (g *GameState) AliveIDs() []int64 {
	var out []int64
	for _, a := range g.Agents {
		if a.Alive {
			out = append(out, a.ID)
		}
	}
	return out
}

// nearby 某 Agent 所在房间的其他 Agent。
func (g *GameState) nearby(id int64) []int64 {
	me := g.Agents[id]
	if me == nil {
		return nil
	}
	var out []int64
	for _, a := range g.Agents {
		if a.ID != id && a.Alive && a.Room == me.Room {
			out = append(out, a.ID)
		}
	}
	return out
}

// log 记录一条世界事件（供 Perceive 展示），同时打印到控制台（观察涌现用）。
func (g *GameState) log(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	g.EventLog = append(g.EventLog, msg)
	if len(g.EventLog) > 50 {
		g.EventLog = g.EventLog[len(g.EventLog)-50:]
	}
	fmt.Println("[game]", msg)
	// M5：发布通用事件到观察台（Agent 推理/决策的原始文本流）
	if g.obs != nil {
		g.obs.Publish("world.event", map[string]interface{}{"text": msg})
	}
}

// publish 发布一条结构化事件到观察台（SSE 驱动前端地图/Timeline）。
func (g *GameState) publish(typ string, data interface{}) {
	if g.obs != nil {
		g.obs.Publish(typ, data)
	}
}

// Obs 返回观察台（HTTP server 读取事件/订阅用）。
func (g *GameState) Obs() *Observatory { return g.obs }

// GameSnapshot 游戏状态快照（观测 API /api/game 用，带锁）。
type GameSnapshot struct {
	Phase   string            `json:"phase"`   // action / meeting / over
	Round   int               `json:"round"`
	Winner  string            `json:"winner,omitempty"`
	EndedBy string            `json:"endedBy,omitempty"`
	Agents  []AgentPublic     `json:"agents"`
	Bodies  []BodyBrief       `json:"bodies"`
}

// AgentPublic 一个 Agent 的公开信息（不含 Belief/Relationship——主观状态私有）。
type AgentPublic struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Team    string `json:"team"`   // 身份（观战默认显示；信息隔离只保护 Agent 之间）
	Alive   bool   `json:"alive"`
	Room    string `json:"room"`
	TaskDone int   `json:"taskDone"`
	// M5.1 2D 空间坐标（前端据此渲染 Agent 在地图上的真实位置）
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Facing float64 `json:"facing"`
}

// Snapshot 返回当前游戏状态的公开快照（带锁）。
func (g *GameState) Snapshot() GameSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	snap := GameSnapshot{
		Phase:   g.Phase.String(),
		Round:   g.Round,
		Winner:  g.Winner.String(),
		EndedBy: g.EndedBy,
	}
	if g.Phase != PhaseOver {
		snap.Winner = ""
	}
	for _, a := range g.Agents {
		snap.Agents = append(snap.Agents, AgentPublic{
			ID: a.ID, Name: a.Name, Team: a.Team.String(),
			Alive: a.Alive, Room: string(a.Room), TaskDone: a.TaskDone,
			X: a.X, Y: a.Y, Facing: a.Facing,
		})
	}
	for _, b := range g.Bodies {
		snap.Bodies = append(snap.Bodies, BodyBrief{AgentID: b.AgentID, Room: b.Room})
	}
	return snap
}

// AgentInspector 单个 Agent 的私有状态（Agent Inspector 用，面向调试）。
// 设计：这是"点开某个 Agent"才按需取回的深度视图。它只含该 Agent 自己的
// 主观状态（Belief/Relationship/Goal/LastDecision）+ 它视角的事件记忆。
// 游戏 UI 默认不展示它；普通观众只看到地图上的行为。
type AgentInspector struct {
	ID           int64              `json:"id"`
	Name         string             `json:"name"`
	Team         string             `json:"team"`
	Alive        bool               `json:"alive"`
	Room         string             `json:"room"`
	Goal         string             `json:"goal"`      // 由身份派生的目标
	LastDecision string             `json:"lastDecision"`
	LastAction   string             `json:"lastAction"`
	Belief       []SuspectJSON      `json:"belief"`       // 该 Agent 对每人的怀疑
	Relationship []RelJSON          `json:"relationship"` // 该 Agent 与每人的好感
	Memory       []string           `json:"memory"`       // 该 Agent 视角的最近事件
}

// SuspectJSON 一条主观怀疑（JSON 化）。
type SuspectJSON struct {
	AgentID   int64   `json:"agentID"`
	Name      string  `json:"name"`
	Suspicion float64 `json:"suspicion"`
}

// RelJSON 一条关系好感（JSON 化）。
type RelJSON struct {
	AgentID  int64   `json:"agentID"`
	Name     string  `json:"name"`
	Goodwill float64 `json:"goodwill"`
}

// Inspector 返回某个 Agent 的深度私有状态（带锁，/api/agents/{id} 用）。
// 只返回该 Agent 自己的主观状态与事件记忆，不暴露其他 Agent 的 Belief。
func (g *GameState) Inspector(id int64) *AgentInspector {
	g.mu.Lock()
	defer g.mu.Unlock()
	me := g.Agents[id]
	if me == nil {
		return nil
	}
	ins := &AgentInspector{
		ID:           me.ID,
		Name:         me.Name,
		Team:         me.Team.String(),
		Alive:        me.Alive,
		Room:         string(me.Room),
		LastDecision: me.LastDecision,
		LastAction:   me.LastAction,
	}
	switch me.Team {
	case TeamDuck:
		ins.Goal = "隐藏身份，消灭鹅群，避免被投出去"
	case TeamDodo:
		ins.Goal = "让自己被投票淘汰（特殊目标）"
	default:
		ins.Goal = "找出并淘汰鸭子，完成任务保护大家"
	}
	for tid, s := range me.Belief.Suspicions {
		if tid == me.ID {
			continue
		}
		ins.Belief = append(ins.Belief, SuspectJSON{AgentID: tid, Name: g.nameOf(tid), Suspicion: s})
	}
	for tid, r := range me.Relationships {
		if tid == me.ID {
			continue
		}
		ins.Relationship = append(ins.Relationship, RelJSON{AgentID: tid, Name: g.nameOf(tid), Goodwill: r})
	}
	ins.Memory = g.RecentEvents(8)
	return ins
}

// PublicAgents 返回所有 Agent 的公开信息（带锁，观测 API /api/agents 用）。
func (g *GameState) PublicAgents() []AgentPublic {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]AgentPublic, 0, len(g.Agents))
	for _, a := range g.Agents {
		out = append(out, AgentPublic{
			ID: a.ID, Name: a.Name, Team: a.Team.String(),
			Alive: a.Alive, Room: string(a.Room), TaskDone: a.TaskDone,
			X: a.X, Y: a.Y, Facing: a.Facing,
		})
	}
	return out
}

// RecentEvents 返回最近 n 条世界事件（Agent 视角，可能是它看到的）。
func (g *GameState) RecentEvents(n int) []string {
	if len(g.EventLog) <= n {
		return g.EventLog
	}
	return g.EventLog[len(g.EventLog)-n:]
}
