package hotel

// HotelRole 一个酒店岗位的能力/职责定义（M8.2）。
// 关键：不把行为写死成 if Role==FrontDesk{CheckIn()}，而是让 Role 进入 Agent Context，
// Planner/引擎根据职责列表自主决定能处理什么、该做什么。
type HotelRole struct {
	Role           string   // welcome / frontdesk / concierge / housekeeping / restaurant / maintenance
	Name           string
	Responsibilities []string // 能处理的职责（如 "check_in"）
	// intents 该岗位能处理的 Guest Intent
	Intents        []string
	// canHandle 供引擎查询该岗位能否处理某 Intent
}

// canHandleIntent 判断岗位能否处理某 Guest Intent。
func (r *HotelRole) canHandleIntent(intent string) bool {
	for _, i := range r.Intents {
		if i == intent {
			return true
		}
	}
	return false
}

// GuestIntent 客人的当前需求。
type GuestIntent string

// 预设 Guest Intent。
const (
	IntentCheckIn     GuestIntent = "check_in"
	IntentCheckOut    GuestIntent = "check_out"
	IntentAskDirection GuestIntent = "ask_direction"
	IntentRoomService GuestIntent = "room_service"
	IntentRestaurant  GuestIntent = "restaurant"
	IntentComplaint   GuestIntent = "complaint"
	IntentGeneralHelp GuestIntent = "general_help"
)

// ParseIntent 把 guest 的自然语言消息解析为 Intent（M8.2 模拟解析，不接 NLP）。
func ParseIntent(message string) GuestIntent {
	m := message
	contains := func(s string) bool {
		return len(m) >= len(s) && indexOf(m, s) >= 0
	}
	switch {
	case contains("入住") || contains("check in") || contains("check-in") || contains("开房"):
		return IntentCheckIn
	case contains("退房") || contains("check out") || contains("check-out"):
		return IntentCheckOut
	case contains("房间") || contains("送餐") || contains("room service"):
		return IntentRoomService
	case contains("餐厅") || contains("吃饭") || contains("点餐") || contains("restaurant"):
		return IntentRestaurant
	case contains("投诉") || contains("complain"):
		return IntentComplaint
	case contains("哪里") || contains("怎么走") || contains("direction") || contains("路") || contains("在哪") || contains("在哪"):
		return IntentAskDirection
	default:
		return IntentGeneralHelp
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// defaultRoles 返回酒店的标准岗位职责表。
func defaultRoles() map[string]*HotelRole {
	return map[string]*HotelRole{
		"welcome": {
			Role: "welcome", Name: "迎宾",
			Responsibilities: []string{"迎接客人", "引导方向", "回答一般问题"},
			Intents: []string{string(IntentGeneralHelp), string(IntentAskDirection)},
		},
		"frontdesk": {
			Role: "frontdesk", Name: "前台",
			Responsibilities: []string{"办理入住", "办理退房", "分配房间", "发放房卡"},
			Intents: []string{string(IntentCheckIn), string(IntentCheckOut)},
		},
		"concierge": {
			Role: "concierge", Name: "礼宾",
			Responsibilities: []string{"行李服务", "指引路线", "预订服务"},
			Intents: []string{string(IntentAskDirection), string(IntentGeneralHelp)},
		},
		"housekeeping": {
			Role: "housekeeping", Name: "客房",
			Responsibilities: []string{"打扫房间", "补充用品", "送餐服务"},
			Intents: []string{string(IntentRoomService)},
		},
		"restaurant": {
			Role: "restaurant", Name: "餐厅",
			Responsibilities: []string{"点餐", "安排座位"},
			Intents: []string{string(IntentRestaurant)},
		},
	}
}
