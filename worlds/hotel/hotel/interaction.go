package hotel

import (
	"sync"
	"time"
)

// SpaceWorld 的 M8.2 扩展：角色 / 意图 / 对话 / 交接 / 任务。
// 这些字段和逻辑让 Agent 真正"工作"。

// roleStore 角色表 + 任务 + 对话历史 + guest 意图（挂载到 SpaceWorld）。
// 通过 world 的扩展方法访问，避免改 core space.go。
type interactionState struct {
	mu      sync.Mutex
	roles   map[string]*HotelRole
	// guestID → 当前 intent
	intents map[int64]GuestIntent
	// guestID → 当前负责 Agent
	handler map[int64]int64
	// 任务
	tasks     []*HotelTask
	nextTask  int64
	// 对话历史（时间线）
	conversation []ConversationMsg
	// 已入住的房间
	checkins map[int64]string
}

// newInteractionState 初始化 M8.2 交互状态。
func newInteractionState() *interactionState {
	return &interactionState{
		roles:        defaultRoles(),
		intents:      map[int64]GuestIntent{},
		handler:      map[int64]int64{},
		checkins:     map[int64]string{},
	}
}

// ---------- 对话 ----------

// Say 让一个 Agent 说话（记入对话历史）。
func (w *SpaceWorld) Say(agentID int64, text string) ConversationMsg {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	msg := ConversationMsg{Time: time.Now(), Speaker: agentID,
		SpeakerName: w.safeName(agentID), Role: w.roleOf(agentID), Text: text}
	w.state.conversation = append(w.state.conversation, msg)
	return msg
}

// GuestSay 让一个 Guest 说话（解析 Intent）。
func (w *SpaceWorld) GuestSay(guestID int64, text string) (GuestIntent, ConversationMsg) {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	intent := ParseIntent(text)
	w.state.intents[guestID] = intent
	msg := ConversationMsg{Time: time.Now(), Speaker: guestID,
		SpeakerName: w.safeName(guestID), Role: "guest", Text: text}
	w.state.conversation = append(w.state.conversation, msg)
	return intent, msg
}

// safeName 取名字。
func (w *SpaceWorld) safeName(id int64) string {
	if g, ok := w.guests[id]; ok {
		return g.Name
	}
	if a, ok := w.agents[id]; ok {
		return a.Name
	}
	return "Entity#" + itoa64(id)
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Conversation 返回对话历史。
func (w *SpaceWorld) Conversation() []ConversationMsg {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	out := make([]ConversationMsg, len(w.state.conversation))
	copy(out, w.state.conversation)
	return out
}

// ---------- Intent / Handoff ----------

// IntentOf 返回 guest 当前意图。
func (w *SpaceWorld) IntentOf(guestID int64) GuestIntent {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	return w.state.intents[guestID]
}

// HandlerOf 返回 guest 当前负责 Agent。
func (w *SpaceWorld) HandlerOf(guestID int64) int64 {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	return w.state.handler[guestID]
}

// HandleIntent M8.2 核心：处理 guest 的意图。
//   - 找到能处理该 Intent 的 Agent（ResolveByIntent，过滤 Busy）
//   - 若当前 handler 已能处理，继续；否则 handoff 给新 Agent
//   - 返回 (处理 Agent, 是否需要 handoff)
func (w *SpaceWorld) HandleIntent(guestID int64, intent GuestIntent) (int64, bool) {
	target := w.resolveByIntent(intent, guestID)
	if target == 0 {
		return 0, false // 无人能处理
	}
	cur := w.HandlerOf(guestID)
	if cur != 0 && cur == target {
		return target, false // 已由正确 Agent 处理
	}
	// 交接：记录新 handler + 事件
	w.state.mu.Lock()
	w.state.handler[guestID] = target
	if cur != 0 {
		w.bus.Publish(Event{Type: "agent.handoff", HotelID: w.space.HotelID(),
			LocationID: w.space.AgentLocation(guestID), SubjectID: guestID, Time: time.Now()})
	}
	w.state.mu.Unlock()
	return target, cur != 0
}

// resolveByIntent 找到能处理该 Intent 的 Agent（复用 Responsibility Resolver 思路 + 岗位能力过滤）。
func (w *SpaceWorld) resolveByIntent(intent GuestIntent, guestID int64) int64 {
	w.state.mu.Lock()
	roles := w.state.roles
	w.state.mu.Unlock()

	// 找到能处理该 intent 的岗位
	var targetRole string
	for _, r := range roles {
		if r.canHandleIntent(string(intent)) {
			targetRole = r.Role
			break
		}
	}
	if targetRole == "" {
		return 0
	}
	// 找该岗位的 Agent（优先非 Busy）
	best := int64(0)
	for id, a := range w.agents {
		if a.Role != targetRole {
			continue
		}
		if w.isBusy(id) {
			continue
		}
		if best == 0 {
			best = id
		}
	}
	return best
}

// ---------- 任务 ----------

// CreateTask 创建一个任务。
func (w *SpaceWorld) CreateTask(typ TaskType, guestID, agentID int64) *HotelTask {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	w.state.nextTask++
	t := NewTask(w.state.nextTask, typ, guestID)
	t.AgentID = agentID
	t.Status = "processing"
	w.state.tasks = append(w.state.tasks, t)
	return t
}

// CompleteTask 完成任务。
func (w *SpaceWorld) CompleteTask(taskID int64, data map[string]interface{}) bool {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	for _, t := range w.state.tasks {
		if t.ID == taskID {
			t.Status = "completed"
			if data != nil {
				for k, v := range data {
					t.Data[k] = v
				}
			}
			return true
		}
	}
	return false
}

// FailTask 标记任务失败。
func (w *SpaceWorld) FailTask(taskID int64) bool {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	for _, t := range w.state.tasks {
		if t.ID == taskID {
			t.Status = "failed"
			return true
		}
	}
	return false
}

// Tasks 返回全部任务。
func (w *SpaceWorld) Tasks() []*HotelTask {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	out := make([]*HotelTask, len(w.state.tasks))
	copy(out, w.state.tasks)
	return out
}

// ---------- M8.4 真实 PMS 发卡 ----------

// RealPMSCheckIn M8.4：接入真实 PMS/门锁 MCP 时，办理入住 = 发放房卡。
// 直接调用 send_room_key（cardType=1 新卡/办理入住，keyKind=1 物理卡），
// 时间格式 YYYY-MM-DD HH:mm:ss。
// 返回 (是否成功, 房间号, 工具结果)。
func (w *SpaceWorld) RealPMSCheckIn(guestID int64, roomNumber, arrival, departure string) (bool, string, map[string]interface{}) {
	if w.pmsMCP == nil {
		return false, "", nil
	}
	result, ok := w.pmsMCP.SendRoomKey(roomNumber, 1, 1, arrival, departure, false)
	if !ok {
		return false, roomNumber, result
	}
	// 记录入住任务完成 + 房卡事件
	task := w.CreateTask(TaskCheckIn, guestID, w.HandlerOf(guestID))
	w.CompleteTask(task.ID, map[string]interface{}{
		"room": roomNumber, "status": "checked_in", "card": result,
	})
	w.Say(w.HandlerOf(guestID), "您好，您的房间 "+roomNumber+" 房卡已发放，祝您入住愉快！")
	return true, roomNumber, result
}

// ---------- M8.3 业务工具驱动入住 ----------

// RunCheckInFlow M8.3：让前台 Planner 自主完成 guest 的入住流程。
// 它根据 guest 的 intent + 进度 + 工具结果，逐步调用 get_reservation / assign_room / check_in / create_key_card。
// 返回 (是否完成, 房间号, 各步骤工具结果)。
func (w *SpaceWorld) RunCheckInFlow(guestID int64, intent GuestIntent) (bool, string, []ToolResult) {
	// 每次调用重置该 guest 的入住进度，重新走完整流程（这样能检测到"已入住"重复办理）
	w.planner.Reset(guestID)
	var results []ToolResult
	for step := 0; step < 8; step++ { // 上限保护，避免死循环
		res, tag := w.planner.NextStep(guestID, intent)
		results = append(results, res)
		switch tag {
		case "no_reservation":
			return false, "", results
		case "no_room":
			return false, "", results
		case "already_checked_in":
			return false, "", results
		case "checked_in":
			// 入住完成，但还要继续生成房卡（下次循环走 done → create_key_card）
			continue
		case "card_created":
			// 全部完成（含房卡）
			_, room := w.planner.Status(guestID)
			return true, room, results
		}
	}
	return false, "", results
}

// ---------- 模拟 Check-in ----------

// CheckIn M8.2 模拟入住（不接 PMS，Hotel World 内存数据）。
// 返回 (任务, 房间号, 是否成功)。
func (w *SpaceWorld) CheckIn(guestID, agentID int64, guestName string) (*HotelTask, string, bool) {
	task := w.CreateTask(TaskCheckIn, guestID, agentID)
	room := w.assignRoom()
	ok := w.CompleteTask(task.ID, map[string]interface{}{
		"guest_name": guestName,
		"room":       room,
		"status":     "checked_in",
	})
	if ok {
		w.state.mu.Lock()
		w.state.checkins[guestID] = room
		w.state.mu.Unlock()
		// 前台确认话语（记入对话时间线）
		w.Say(agentID, "您好"+guestName+"，您已入住房间 "+room+"，祝您入住愉快！")
	}
	return task, room, ok
}

// assignRoom 分配房间（模拟：从可用房间池选）。
func (w *SpaceWorld) assignRoom() string {
	// 简单模拟：固定从 101 开始
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	taken := map[string]bool{}
	for _, r := range w.state.checkins {
		taken[r] = true
	}
	for n := 101; n <= 199; n++ {
		room := itoa(n)
		if !taken[room] {
			return room
		}
	}
	return "201"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
