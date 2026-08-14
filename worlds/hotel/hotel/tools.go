package hotel

import "time"

// ---- M8.3 Hotel Tool 层 ----
// Agent 不直接操作数据库/PMS，而是通过 HotelTool 调用业务能力。
// 以后接真实 PMS，只替换 Tool Backend（不污染 Agent）。

// ToolBackend 统一工具后端接口（Mock PMS 实现；未来真实 PMS 实现）。
type ToolBackend interface {
	// GetReservation 查询预订（按预订号或 guest）
	GetReservation(id string, guestID int64) *Reservation
	// GetRoom 查询房间
	GetRoom(number string) *Room
	// AvailableRooms 返回某房型可用房间
	AvailableRooms(roomType string) []string
	// AssignRoom 分配房间
	AssignRoom(number string) bool
	// CheckIn 办理入住
	CheckIn(reservationID, roomNumber string, guestID int64) (string, bool)
	// CheckOut 退房
	CheckOut(reservationID string) bool
	// CreateKeyCard 生成房卡
	CreateKeyCard(guestID int64, roomNumber string) *KeyCard
}

// ToolResult 一次工具调用的结果（进入 Agent Perception）。
type ToolResult struct {
	Tool    string      `json:"tool"`
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	At      time.Time   `json:"at"`
}

// ToolLog 一次工具调用的记录（Timeline/Audit）。
type ToolLog struct {
	Tool     string    `json:"tool"`
	Success  bool      `json:"success"`
	Message  string    `json:"message"`
	AgentID  int64     `json:"agent_id"`
	Time     time.Time `json:"time"`
}

// HotelTool M8.3：酒店业务工具层（前端接待 Agent 使用）。
type HotelTool struct {
	backend ToolBackend
	logs    []ToolLog
}

// NewHotelTool 创建工具层。
func NewHotelTool(backend ToolBackend) *HotelTool {
	return &HotelTool{backend: backend, logs: []ToolLog{}}
}

// Backend 返回后端（供测试/替换）。
func (t *HotelTool) Backend() ToolBackend { return t.backend }

// Logs 返回工具调用记录（Audit/Timeline）。
func (t *HotelTool) Logs() []ToolLog { return t.logs }

// GetReservation 查询预订。
func (t *HotelTool) GetReservation(id string, guestID int64) ToolResult {
	r := t.backend.GetReservation(id, guestID)
	if r == nil {
		return t.record("get_reservation", false, "未找到预订", nil)
	}
	return t.record("get_reservation", true, "找到预订 "+r.ID, r)
}

// GetRoom 查询房间。
func (t *HotelTool) GetRoom(number string) ToolResult {
	r := t.backend.GetRoom(number)
	if r == nil {
		return t.record("get_room", false, "房间不存在", nil)
	}
	return t.record("get_room", true, "房间 "+r.Number+" "+r.Status, r)
}

// GetAvailableRooms 查可用房间。
func (t *HotelTool) GetAvailableRooms(roomType string) ToolResult {
	rooms := t.backend.AvailableRooms(roomType)
	if len(rooms) == 0 {
		return t.record("get_available_rooms", false, "没有可用房间", rooms)
	}
	return t.record("get_available_rooms", true, "可用房间: "+join(rooms), rooms)
}

// AssignRoom 分配房间。
func (t *HotelTool) AssignRoom(number string) ToolResult {
	if !t.backend.AssignRoom(number) {
		return t.record("assign_room", false, "房间 "+number+" 不可分配", nil)
	}
	return t.record("assign_room", true, "房间 "+number+" 已分配", map[string]string{"room": number})
}

// CheckIn 办理入住。
func (t *HotelTool) CheckIn(reservationID, roomNumber string, guestID int64) ToolResult {
	stayID, ok := t.backend.CheckIn(reservationID, roomNumber, guestID)
	if !ok {
		return t.record("check_in", false, "入住失败（无此预订或房间未分配或已入住）", nil)
	}
	return t.record("check_in", true, "入住成功 "+reservationID+" → "+roomNumber,
		map[string]string{"stay": stayID, "room": roomNumber})
}

// CheckOut 退房。
func (t *HotelTool) CheckOut(reservationID string) ToolResult {
	if !t.backend.CheckOut(reservationID) {
		return t.record("check_out", false, "退房失败", nil)
	}
	return t.record("check_out", true, "退房成功 "+reservationID, nil)
}

// CreateKeyCard 生成房卡。
func (t *HotelTool) CreateKeyCard(guestID int64, roomNumber string) ToolResult {
	card := t.backend.CreateKeyCard(guestID, roomNumber)
	return t.record("create_key_card", true, "房卡 "+card.CardID+" 已生成", card)
}

// record 记录工具调用（Audit）。
func (t *HotelTool) record(tool string, success bool, msg string, data interface{}) ToolResult {
	t.logs = append(t.logs, ToolLog{Tool: tool, Success: success, Message: msg, Time: time.Now()})
	return ToolResult{Tool: tool, Success: success, Message: msg, Data: data, At: time.Now()}
}

func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
