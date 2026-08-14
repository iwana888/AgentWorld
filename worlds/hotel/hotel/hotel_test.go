package hotel

import (
	"testing"
	"time"
)

// newTestHotel 构建测试酒店：Entrance → Lobby → FrontDesk/…；Alice(welcome, Entrance, P100)、
// Tom(welcome, Entrance, P80)、Bob(frontdesk, FrontDesk, P100)。
func newTestHotel() *SpaceWorld {
	w := NewSpaceWorld("hotel001", "Test Hotel", "测试酒店")
	s := w.Space()
	s.AddLocation(&Location{ID: "entrance", Name: "Entrance", Type: "entrance", X: 0, Y: 0})
	s.AddLocation(&Location{ID: "lobby", Name: "Lobby", Type: "lobby", X: 0, Y: 2})
	s.AddLocation(&Location{ID: "frontdesk", Name: "FrontDesk", Type: "frontdesk", X: 2, Y: 2})
	s.AddLocation(&Location{ID: "restaurant", Name: "Restaurant", Type: "restaurant", X: -2, Y: 3})
	s.AddLocation(&Location{ID: "elevator", Name: "Elevator", Type: "elevator", X: 0, Y: 4})
	s.Connect("entrance", "lobby")
	s.Connect("lobby", "frontdesk")
	s.Connect("lobby", "restaurant")
	s.Connect("lobby", "elevator")

	w.AddAgent(&Agent{ID: 1, Kind: "ai", Name: "Alice", Role: "welcome", HotelID: "hotel001", Location: "entrance"})
	w.SetAgentRole(1, "welcome", "entrance", 100)
	w.AddAgent(&Agent{ID: 2, Kind: "ai", Name: "Tom", Role: "welcome", HotelID: "hotel001", Location: "entrance"})
	w.SetAgentRole(2, "welcome", "entrance", 80)
	w.AddAgent(&Agent{ID: 3, Kind: "ai", Name: "Bob", Role: "frontdesk", HotelID: "hotel001", Location: "frontdesk"})
	w.SetAgentRole(3, "frontdesk", "frontdesk", 100)
	return w
}

// TestSpatialModel 验收 Test 1：1 个酒店 ≥5 个位置 + 连接正确。
func TestSpatialModel(t *testing.T) {
	w := newTestHotel()
	if got := len(w.Space().Locations()); got < 5 {
		t.Errorf("should have >=5 locations, got %d", got)
	}
	// 连接正确：entrance → lobby 可到；entrance → frontdesk 不可直接到
	if !w.Space().CanMove("entrance", "lobby") {
		t.Error("entrance should connect to lobby")
	}
	if w.Space().CanMove("entrance", "frontdesk") {
		t.Error("entrance should NOT directly connect to frontdesk")
	}
}

// TestAgentPositioning 验收 Test 2：Agent 位置正确。
func TestAgentPositioning(t *testing.T) {
	w := newTestHotel()
	if got := w.Space().AgentLocation(1); got != "entrance" {
		t.Errorf("Alice should be at entrance, got %s", got)
	}
	if got := w.Space().AgentLocation(3); got != "frontdesk" {
		t.Errorf("Bob should be at frontdesk, got %s", got)
	}
}

// TestGuestEnteredPerception 验收 Test 3：Guest 进入 Entrance，Alice 感知，Bob 不感知。
func TestGuestEnteredPerception(t *testing.T) {
	w := newTestHotel()
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Guest1", Location: "entrance"}
	responsible := w.AddGuest(g)
	// 负责 Entrance 的是 welcome Agent：Alice(P100) 或 Tom(P80)，取 P100 的 Alice
	if responsible != 1 {
		t.Errorf("Alice(P100) should be responsible, got %d", responsible)
	}
	// Alice 感知到附近有 Guest
	alice := w.Perception(1)
	nearby, _ := alice["nearby"].([]NearbyEntity)
	if len(nearby) == 0 {
		t.Error("Alice should perceive nearby guest")
	}
	// Bob（在 frontdesk）不应感知到 entrance 的 Guest（Bob 附近只有自己，不含 guest 1001）
	bob := w.Perception(3)
	bobNearby, _ := bob["nearby"].([]NearbyEntity)
	for _, e := range bobNearby {
		if e.AgentID == 1001 {
			t.Error("Bob at frontdesk should NOT perceive entrance guest")
		}
	}
}

// TestGuestMovement 验收 Test 4：Guest Entrance → Lobby → FrontDesk，负责 Agent 切换。
func TestGuestMovement(t *testing.T) {
	w := newTestHotel()
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Guest1", Location: "entrance"}
	_ = w.AddGuest(g)
	// Guest 移到 Lobby（welcome Agent 负责）
	if ok, _ := w.MoveTo(1001, "lobby"); !ok {
		t.Fatal("guest should move entrance→lobby")
	}
	// 此时负责 Lobby 的 welcome Agent 仍应感知（Alice 虽负责 entrance，但 guest 在 lobby）
	// M8.1 验收重点：FrontDesk Agent 在 guest 到 frontdesk 后才感知
	if ok, _ := w.MoveTo(1001, "frontdesk"); !ok {
		t.Fatal("guest should move lobby→frontdesk")
	}
	// Bob 现在应感知到 guest
	bobNearby, _ := w.Perception(3)["nearby"].([]NearbyEntity)
	found := false
	for _, e := range bobNearby {
		if e.AgentID == 1001 {
			found = true
		}
	}
	if !found {
		t.Error("Bob at frontdesk should perceive guest after guest moved to frontdesk")
	}
}

// TestAgentMove 验收 Test 5：Alice Entrance → Lobby，位置更新。
func TestAgentMove(t *testing.T) {
	w := newTestHotel()
	if ok, _ := w.MoveTo(1, "lobby"); !ok {
		t.Fatal("Alice should move entrance→lobby")
	}
	if got := w.Space().AgentLocation(1); got != "lobby" {
		t.Errorf("Alice should now be at lobby, got %s", got)
	}
	// 非法移动：lobby → entrance 允许（有连接）；但 entrance → frontdesk 不允许
	if ok, _ := w.MoveTo(1, "entrance"); !ok {
		t.Error("lobby→entrance should be allowed")
	}
	if ok, _ := w.MoveTo(1, "frontdesk"); ok {
		t.Error("entrance→frontdesk should be rejected (no connection)")
	}
}

// TestResolverMultiAgent 验收 Test 6：Alice(P100) vs Tom(P80)，Guest 进入，选择 P100 的 Alice。
func TestResolverMultiAgent(t *testing.T) {
	w := newTestHotel()
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Guest1", Location: "entrance"}
	if got := w.AddGuest(g); got != 1 {
		t.Errorf("resolver should pick Alice(P100), got %d", got)
	}
}

// TestResolverBusyFilter 验收 Test 7：Alice Busy，Guest 进入 Entrance，选择 Tom。
func TestResolverBusyFilter(t *testing.T) {
	w := newTestHotel()
	// Alice 忙碌
	w.SetAgentBusy(1, time.Now().Add(time.Hour))
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "Guest1", Location: "entrance"}
	if got := w.AddGuest(g); got != 2 {
		t.Errorf("resolver should pick Tom when Alice busy, got %d", got)
	}
}
