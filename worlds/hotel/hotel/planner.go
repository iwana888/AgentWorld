package hotel

// ---- M8.3 FrontDesk 自主决策 ----
// 不写死 if check_in { get_reservation(); assign_room(); check_in(); }。
// Planner 根据 Guest Intent + 当前入住进度 + 上一次工具结果，自主决定下一步调用哪个工具。

// checkInProgress 一次入住流程的进度状态。
type checkInProgress struct {
	guestID     int64
	reservation *Reservation // 查到的预订
	room        string       // 已分配房间
	step        string       // start / wait_reservation / wait_room / room_assigned / done / error
	lastError   string
}

// FrontDeskPlanner M8.3：前台接待的自主决策引擎。
// 持有一个工具层，根据状态机推进入住流程（但仍由"状态+工具结果"驱动，可扩展新逻辑）。
type FrontDeskPlanner struct {
	tools  *HotelTool
	// guestID → 入住进度
	progress map[int64]*checkInProgress
}

// NewFrontDeskPlanner 创建前台决策引擎。
func NewFrontDeskPlanner(tools *HotelTool) *FrontDeskPlanner {
	return &FrontDeskPlanner{tools: tools, progress: map[int64]*checkInProgress{}}
}

// NextStep 根据 guest 当前进度，决定下一步调用的工具并执行。
// 返回 (工具名, 结果, 是否还需要继续/完成)。
// 这模拟了 Agent "自主决定下一步"：它看到当前状态 + 工具结果，选择下一个动作。
func (p *FrontDeskPlanner) NextStep(guestID int64, intent GuestIntent) (ToolResult, string) {
	prog := p.progress[guestID]
	if prog == nil {
		// 新 guest 办理入住：先查预订
		if intent == IntentCheckIn {
			prog = &checkInProgress{guestID: guestID, step: "start"}
			p.progress[guestID] = prog
			res := p.tools.GetReservation("", guestID)
			if res.Success {
				r := res.Data.(*Reservation)
				// 重复入住检测：预订已 checked_in → 不能重复办理
				if r.Status == "checked_in" {
					prog.step = "error"
					prog.lastError = "already_checked_in"
					return res, "already_checked_in"
				}
				prog.reservation = r
				prog.step = "wait_room"
				return res, "reservation_found"
			}
			prog.step = "error"
			prog.lastError = "no_reservation"
			return res, "no_reservation"
		}
		return ToolResult{}, ""
	}

	switch prog.step {
	case "wait_room":
		// 预订确认后：查可用房间
		roomType := ""
		if prog.reservation != nil {
			roomType = prog.reservation.RoomType
		}
		res := p.tools.GetAvailableRooms(roomType)
		if res.Success {
			rooms := res.Data.([]string)
			if len(rooms) > 0 {
				// 分配第一间
				assign := p.tools.AssignRoom(rooms[0])
				if assign.Success {
					prog.room = rooms[0]
					prog.step = "room_assigned"
					return assign, "room_assigned"
				}
			}
		}
		prog.step = "error"
		prog.lastError = "no_room"
		return res, "no_room"

	case "room_assigned":
		// 房间已分配：办理入住
		res := p.tools.CheckIn(prog.reservation.ID, prog.room, guestID)
		if res.Success {
			prog.step = "done"
			return res, "checked_in"
		}
		// 已入住（重复入住）异常
		prog.step = "error"
		prog.lastError = "already_checked_in"
		return res, "already_checked_in"

	case "done":
		// 入住完成：生成房卡
		res := p.tools.CreateKeyCard(guestID, prog.room)
		return res, "card_created"
	}
	return ToolResult{}, ""
}

// Reset 重置某个 guest 的入住进度（供重新走流程 / 重复入住检测）。
func (p *FrontDeskPlanner) Reset(guestID int64) {
	delete(p.progress, guestID)
}

// Status 返回 guest 的入住进度（供 API/Why 展示）。
func (p *FrontDeskPlanner) Status(guestID int64) (string, string) {
	prog := p.progress[guestID]
	if prog == nil {
		return "none", ""
	}
	return prog.step, prog.room
}
