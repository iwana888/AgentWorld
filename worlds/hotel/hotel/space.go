// Package hotel —— Hotel World：一个让 Agent 拥有物理位置、空间感知、岗位责任区域的酒店空间世界。
//
// 核心原则（M8.1）：
//   - Location 是一等数据：位置决定"在哪里"，岗位决定"负责什么"，两者分离。
//   - Agent 不直接看到整个酒店，只看到：自己位置、附近对象、附近事件、自己的负责区域、可到达位置。
//   - Guest 不单独造一套体系：Guest = Agent + Hotel Context（真人/AI 客人都可）。
//   - 复用现有 Economy 的 BusyUntil（Busy 状态），连接 Hotel World 与 Economy World。
package hotel

import (
	"math"
	"sync"
)

// Space 一个酒店空间世界：位置 + 连接 + Agent 位置 + 责任区域。
type Space struct {
	mu sync.Mutex

	hotelID     string
	name        string
	description string

	// 位置：id → Location（含坐标）
	locations map[string]*Location
	// 连接：from → to（可到达）
	connections map[string][]string

	// Agent 位置：agentID → locationID
	agentLoc map[int64]string
	// 责任区域：agentID → Responsibility
	responsibilities map[int64]*Responsibility
}

// NewSpace 创建酒店空间世界。
func NewSpace(hotelID, name, description string) *Space {
	return &Space{
		hotelID:          hotelID,
		name:             name,
		description:      description,
		locations:        map[string]*Location{},
		connections:      map[string][]string{},
		agentLoc:         map[int64]string{},
		responsibilities: map[int64]*Responsibility{},
	}
}

// HotelID 返回酒店 ID。
func (s *Space) HotelID() string { return s.hotelID }

// Name 返回酒店名。
func (s *Space) Name() string { return s.name }

// Location 一个空间位置（二维坐标，不做 3D）。
type Location struct {
	ID       string
	Name     string
	Type     string // entrance / lobby / frontdesk / restaurant / elevator / room / corridor ...
	X, Y     float64
	HotelID  string
	ZoneID   string
	ParentID string // 父位置（可选）
}

// AddLocation 添加一个位置。
func (s *Space) AddLocation(loc *Location) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.locations[loc.ID] = loc
}

// Location 返回位置。
func (s *Space) Location(id string) *Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locations[id]
}

// Locations 返回全部位置。
func (s *Space) Locations() []*Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Location, 0, len(s.locations))
	for _, l := range s.locations {
		cp := *l
		out = append(out, &cp)
	}
	return out
}

// Connect 在两个位置之间建立连接（双向）。
func (s *Space) Connect(from, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[from] = append(s.connections[from], to)
	s.connections[to] = append(s.connections[to], from)
}

// CanMove 判断能否从 from 直接移动到 to（必须存在连接）。
func (s *Space) CanMove(from, to string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.connections[from] {
		if d == to {
			return true
		}
	}
	return false
}

// Reachable 返回某位置可到达的位置列表。
func (s *Space) Reachable(from string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.connections[from]))
	out = append(out, s.connections[from]...)
	return out
}

// SetAgentLocation 设置 Agent 的位置。
func (s *Space) SetAgentLocation(agentID int64, locationID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentLoc[agentID] = locationID
}

// AgentLocation 返回 Agent 的位置。
func (s *Space) AgentLocation(agentID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentLoc[agentID]
}

// AgentsAt 返回某位置的 Agent ID 列表。
func (s *Space) AgentsAt(locationID string) []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int64
	for aid, lid := range s.agentLoc {
		if lid == locationID {
			out = append(out, aid)
		}
	}
	return out
}

// SetResponsibility 设置 Agent 的责任区域。
func (s *Space) SetResponsibility(agentID int64, resp *Responsibility) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responsibilities[agentID] = resp
}

// ResponsibilityOf 返回 Agent 的责任区域。
func (s *Space) ResponsibilityOf(agentID int64) *Responsibility {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.responsibilities[agentID]; ok {
		cp := *r
		return &cp
	}
	return nil
}

// Responsibilities 返回全部责任区域。
func (s *Space) Responsibilities() map[int64]*Responsibility {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64]*Responsibility{}
	for aid, r := range s.responsibilities {
		cp := *r
		out[aid] = &cp
	}
	return out
}

// Distance 返回两个位置的欧氏距离（第一版用 2D 坐标，以后可换真实地图距离）。
func (s *Space) Distance(fromID, toID string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.locations[fromID]
	t := s.locations[toID]
	if f == nil || t == nil {
		return math.MaxFloat64
	}
	dx := f.X - t.X
	dy := f.Y - t.Y
	return math.Sqrt(dx*dx + dy*dy)
}
