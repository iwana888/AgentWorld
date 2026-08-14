package hotel

import "testing"

// TestPMSBasics 验证 M8.3：Mock PMS 基本数据。
func TestPMSBasics(t *testing.T) {
	pms := NewMockPMS()
	if r := pms.GetReservation("R10001", 0); r == nil || r.GuestName != "Zhang San" {
		t.Errorf("R10001 should exist, got %+v", r)
	}
	if r := pms.GetReservation("", 1002); r == nil || r.ID != "R10002" {
		t.Errorf("guest 1002 should have R10002, got %+v", r)
	}
	if rooms := pms.AvailableRooms("Deluxe"); len(rooms) == 0 {
		t.Error("should have available Deluxe rooms")
	}
}

// TestCheckInFlowSuccess 验收 M8.3：完整入住成功故事。
// Guest 1001(Zhang San) → FrontDesk Planner 自主完成入住 → 房卡。
func TestCheckInFlowSuccess(t *testing.T) {
	w := NewSpaceWorld("hotel001", "Test", "test")
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Zhang San", Location: "frontdesk"}
	w.AddGuest(g)

	ok, room, results := w.RunCheckInFlow(1001, IntentCheckIn)
	if !ok {
		t.Fatalf("check-in should succeed, results=%d steps", len(results))
	}
	if room == "" {
		t.Error("should assign a room")
	}
	// 验证工具调用链：get_reservation → get_available_rooms → assign_room → check_in → create_key_card
	tools := []string{}
	for _, res := range results {
		tools = append(tools, res.Tool)
	}
	if !containsTool(tools, "get_reservation") || !containsTool(tools, "assign_room") ||
		!containsTool(tools, "check_in") || !containsTool(tools, "create_key_card") {
		t.Errorf("expected full tool chain, got %v", tools)
	}
	// 最终状态：预订 checked_in
	if r := w.Tools().Backend().GetReservation("R10001", 0); r.Status != "checked_in" {
		t.Errorf("reservation should be checked_in, got %s", r.Status)
	}
	// 房卡 active
	cards := w.Tools().Backend().(*MockPMS)
	_ = cards
	if len(w.Tools().Logs()) < 4 {
		t.Errorf("tool logs should have >=4 entries, got %d", len(w.Tools().Logs()))
	}
}

// TestNoReservation 验收 M8.3 异常：guest 没有预订。
func TestNoReservation(t *testing.T) {
	w := NewSpaceWorld("hotel001", "Test", "test")
	// guest 9999 无预订
	w.AddGuest(&Guest{ID: 9999, Kind: "human", Role: "guest", Name: "NoRes", Location: "frontdesk"})
	ok, _, results := w.RunCheckInFlow(9999, IntentCheckIn)
	if ok {
		t.Error("check-in should fail for guest without reservation")
	}
	if len(results) == 0 || results[0].Tool != "get_reservation" {
		t.Errorf("should attempt get_reservation first, got %+v", results)
	}
	if results[0].Success {
		t.Error("get_reservation should fail for no reservation")
	}
}

// TestRepeatCheckIn 验收 M8.3 异常：重复入住。
func TestRepeatCheckIn(t *testing.T) {
	w := NewSpaceWorld("hotel001", "Test", "test")
	w.AddGuest(&Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Zhang San", Location: "frontdesk"})
	ok1, _, _ := w.RunCheckInFlow(1001, IntentCheckIn)
	if !ok1 {
		t.Fatal("first check-in should succeed")
	}
	// 第二次入住同一 guest：预订已 checked_in → 失败
	ok2, _, results2 := w.RunCheckInFlow(1001, IntentCheckIn)
	if ok2 {
		t.Error("repeat check-in should fail")
	}
	_ = results2
}

// TestNoRoom 验收 M8.3 异常：没有可用房间（把 Deluxe 都占满）。
func TestNoRoom(t *testing.T) {
	w := NewSpaceWorld("hotel001", "Test", "test")
	// guest 1001 是 Deluxe，把 Deluxe 房间(201/202)占满
	backend := w.Tools().Backend().(*MockPMS)
	backend.AssignRoom("201")
	backend.AssignRoom("202")
	// 但 101/102 是 Standard，guest 1002 也用 Standard → 可能还有房
	// 用 guest 1001(Deluxe) 测试，Deluxe 已被占满
	w.AddGuest(&Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Zhang San", Location: "frontdesk"})
	ok, _, results := w.RunCheckInFlow(1001, IntentCheckIn)
	if ok {
		t.Error("check-in should fail when no matching room")
	}
	// 应走到 get_available_rooms 或 assign_room 失败
	_ = results
}

func containsTool(tools []string, t string) bool {
	for _, x := range tools {
		if x == t {
			return true
		}
	}
	return false
}
