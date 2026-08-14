package hotel

import "time"

// TaskType 酒店任务类型。
type TaskType string

// 预设任务类型。
const (
	TaskCheckIn    TaskType = "check_in"
	TaskCheckOut   TaskType = "check_out"
	TaskGuestRequest TaskType = "guest_request"
	TaskRoomService TaskType = "room_service"
	TaskComplaint  TaskType = "complaint"
)

// HotelTask 一个酒店任务（M8.2）。
// 状态：pending → processing → completed / failed
type HotelTask struct {
	ID        int64
	Type      TaskType
	GuestID   int64
	AgentID   int64     // 当前处理 Agent（0 = 未分配）
	Status    string    // pending / processing / completed / failed
	CreatedAt time.Time
	Data      map[string]interface{} // 任务数据（如入住信息、房间号）
}

// ConversationMsg 一条对话消息。
type ConversationMsg struct {
	Time     time.Time
	Speaker  int64    // 说话者 AgentID（0 = guest 的负数编码不在此用）
	SpeakerName string
	Role     string   // welcome / frontdesk / guest
	Text     string
}

// NewTask 创建任务。
func NewTask(id int64, typ TaskType, guestID int64) *HotelTask {
	return &HotelTask{ID: id, Type: typ, GuestID: guestID,
		Status: "pending", CreatedAt: time.Now(), Data: map[string]interface{}{}}
}
