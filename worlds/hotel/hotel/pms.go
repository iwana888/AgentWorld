package hotel

import (
	"sync"
	"time"
)

// ---- M8.3 Mock PMS 数据模型 ----
// 这些是 Hotel World 的内存数据，模拟真实 PMS。以后接真实 PMS 只替换 Tool Backend。

// Reservation 预订。
type Reservation struct {
	ID         string // R10001
	GuestName  string // Zhang San
	RoomType   string // Deluxe / Standard
	Status     string // confirmed / checked_in / checked_out / cancelled
	GuestID    int64
}

// Room 房间。
type Room struct {
	Number string // 101
	Type   string // Deluxe / Standard
	Status string // available / occupied / cleaning / maintenance
}

// Stay 入住记录。
type Stay struct {
	ID          string
	Reservation string
	GuestID     int64
	RoomNumber  string
	CheckInAt   time.Time
	CheckOutAt  time.Time
}

// KeyCard 房卡（第一阶段只生成数据，不接真实门锁）。
type KeyCard struct {
	CardID   string
	GuestID  int64
	RoomID   string
	Status   string // active / inactive
	CreatedAt time.Time
}

// MockPMS 一个内存版酒店管理系统（Mock Backend）。
type MockPMS struct {
	mu           sync.Mutex
	reservations map[string]*Reservation
	rooms        map[string]*Room
	stays        map[string]*Stay
	keyCards     map[string]*KeyCard
	nextCard     int
}

// NewMockPMS 创建 Mock PMS，带一批初始预订和房间。
func NewMockPMS() *MockPMS {
	p := &MockPMS{
		reservations: map[string]*Reservation{},
		rooms:        map[string]*Room{},
		stays:        map[string]*Stay{},
		keyCards:     map[string]*KeyCard{},
		nextCard:     1,
	}
	// 房间（101/102/201/202）
	for _, n := range []string{"101", "102", "201", "202"} {
		typ := "Standard"
		if n[0] == '2' {
			typ = "Deluxe"
		}
		p.rooms[n] = &Room{Number: n, Type: typ, Status: "available"}
	}
	// 预订
	p.reservations["R10001"] = &Reservation{ID: "R10001", GuestName: "Zhang San", RoomType: "Deluxe", Status: "confirmed", GuestID: 1001}
	p.reservations["R10002"] = &Reservation{ID: "R10002", GuestName: "Li Si", RoomType: "Standard", Status: "confirmed", GuestID: 1002}
	return p
}

// GetReservation 按预订号或 guest 查预订（匹配 ToolBackend 接口签名）。
func (p *MockPMS) GetReservation(id string, guestID int64) *Reservation {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != "" {
		if r, ok := p.reservations[id]; ok {
			cp := *r
			return &cp
		}
		return nil
	}
	// 按 guest 查
	for _, r := range p.reservations {
		if r.GuestID == guestID && r.Status != "cancelled" {
			cp := *r
			return &cp
		}
	}
	return nil
}

// GetRoom 查房间。
func (p *MockPMS) GetRoom(number string) *Room {
	p.mu.Lock()
	defer p.mu.Unlock()
	if r, ok := p.rooms[number]; ok {
		cp := *r
		return &cp
	}
	return nil
}

// AvailableRooms 返回某房型可用的房间。
func (p *MockPMS) AvailableRooms(roomType string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, r := range p.rooms {
		if r.Status == "available" && (roomType == "" || r.Type == roomType) {
			out = append(out, r.Number)
		}
	}
	return out
}

// AssignRoom 分配房间（可用 → occupied）。
func (p *MockPMS) AssignRoom(number string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.rooms[number]
	if !ok || r.Status != "available" {
		return false
	}
	r.Status = "occupied"
	return true
}

// CheckIn 办理入住：更新预订状态 + 创建入住记录。
func (p *MockPMS) CheckIn(reservationID, roomNumber string, guestID int64) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.reservations[reservationID]
	if !ok {
		return "", false
	}
	if r.Status == "checked_in" {
		return "", false // 已入住，不能重复
	}
	room, ok := p.rooms[roomNumber]
	if !ok || room.Status != "occupied" {
		return "", false // 房间必须已分配
	}
	r.Status = "checked_in"
	stayID := "S" + itoa(len(p.stays)+1)
	p.stays[stayID] = &Stay{ID: stayID, Reservation: reservationID,
		GuestID: guestID, RoomNumber: roomNumber, CheckInAt: time.Now()}
	return stayID, true
}

// CheckOut 办理退房。
func (p *MockPMS) CheckOut(reservationID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.reservations[reservationID]
	if !ok {
		return false
	}
	r.Status = "checked_out"
	return true
}

// CreateKeyCard 生成房卡（不接真实门锁）。
func (p *MockPMS) CreateKeyCard(guestID int64, roomNumber string) *KeyCard {
	p.mu.Lock()
	defer p.mu.Unlock()
	cardID := "C" + itoa(1000+p.nextCard)
	p.nextCard++
	card := &KeyCard{CardID: cardID, GuestID: guestID, RoomID: roomNumber,
		Status: "active", CreatedAt: time.Now()}
	p.keyCards[cardID] = card
	return card
}
