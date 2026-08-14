package hotel

import "time"

// Guest 一个客人（M8.1：Guest = Agent + Hotel Context，不单独造体系）。
// 真人 / AI 客人都可。
type Guest struct {
	ID       int64  // 对应 AgentWorld AgentID
	Kind     string // human / ai
	Role     string // guest
	Name     string
	HotelID  string
	Location string // 当前所在位置
}

// Agent 一个酒店 Agent（员工，或未来的机器人）。
// 空间属性：HotelID / LocationID / Role。
type Agent struct {
	ID     int64
	Kind   string // ai / human
	Name   string
	Role   string // welcome / frontdesk / housekeeping / maintenance ...
	HotelID string
	Location string
	BusyUntil time.Time // 复用 Economy 的 Busy 状态（M6.2.1）
}

// SpaceWorld M8.1 酒店空间世界状态。
type SpaceWorld struct {
	space *Space
	bus   *EventBus
	resolver *Resolver

	agents map[int64]*Agent // 酒店员工
	guests map[int64]*Guest // 客人
}

// NewSpaceWorld 创建酒店空间世界。
func NewSpaceWorld(hotelID, name, description string) *SpaceWorld {
	space := NewSpace(hotelID, name, description)
	return &SpaceWorld{
		space:    space,
		bus:      NewEventBus(),
		agents:   map[int64]*Agent{},
		guests:   map[int64]*Guest{},
	}
}

// Space 返回空间。
func (w *SpaceWorld) Space() *Space { return w.space }

// Events 返回事件总线。
func (w *SpaceWorld) Events() *EventBus { return w.bus }

// Resolver 返回责任解析器。
func (w *SpaceWorld) Resolver() *Resolver {
	if w.resolver == nil {
		w.resolver = NewResolver(w.space)
	}
	return w.resolver
}

// Agent 返回酒店员工。
func (w *SpaceWorld) Agent(agentID int64) *Agent { return w.agents[agentID] }

// Agents 返回全部员工。
func (w *SpaceWorld) Agents() map[int64]*Agent { return w.agents }

// AddAgent 添加一个酒店员工。
func (w *SpaceWorld) AddAgent(a *Agent) {
	w.agents[a.ID] = a
	w.space.SetAgentLocation(a.ID, a.Location)
}

// SetAgentRole 设置员工岗位 + 责任区域。
func (w *SpaceWorld) SetAgentRole(agentID int64, role, location string, priority int) {
	if a, ok := w.agents[agentID]; ok {
		a.Role = role
		a.Location = location
		w.space.SetAgentLocation(agentID, location)
		w.space.SetResponsibility(agentID, &Responsibility{
			AgentID: agentID, HotelID: a.HotelID, Location: location, Role: role, Priority: priority,
		})
	}
}

// SetAgentBusy 设置员工 Busy 状态（复用 Economy 的 BusyUntil 概念）。
func (w *SpaceWorld) SetAgentBusy(agentID int64, until time.Time) {
	if a, ok := w.agents[agentID]; ok {
		a.BusyUntil = until
	}
}

// isBusy 判断员工是否 Busy（供 Resolver 过滤）。
func (w *SpaceWorld) isBusy(agentID int64) bool {
	if a, ok := w.agents[agentID]; ok {
		return time.Now().Before(a.BusyUntil)
	}
	return false
}

// AddGuest 一个 Guest 进入酒店（person.entered 事件触发 + 责任解析）。
// 返回负责感知该 Guest 的员工 Agent ID（0 = 无人负责）。
func (w *SpaceWorld) AddGuest(g *Guest) int64 {
	if g.Location == "" {
		g.Location = "entrance" // 默认从入口进入
	}
	w.guests[g.ID] = g
	w.space.SetAgentLocation(g.ID, g.Location)
	// 触发 person.entered + 责任解析
	w.bus.Publish(Event{Type: "person.entered", HotelID: w.space.HotelID(),
		LocationID: g.Location, SubjectID: g.ID, SubjectKind: g.Kind, Time: time.Now()})
	responsible, _ := w.Resolver().Resolve(g.Location, w.isBusy)
	return responsible
}

// Guest 返回客人。
func (w *SpaceWorld) Guest(id int64) *Guest { return w.guests[id] }

// Guests 返回全部客人。
func (w *SpaceWorld) Guests() map[int64]*Guest { return w.guests }

// MoveTo M8.1：Agent/Guest 移动到目标位置。
//   - 只允许移动到有连接的位置（Entrance → Lobby 允许；Entrance → Room201 拒绝）
//   - 移动成功后触发 agent.left / agent.entered（或 person.*）
//   - 返回 (是否成功, 说明)
func (w *SpaceWorld) MoveTo(agentID int64, target string) (bool, string) {
	cur := w.space.AgentLocation(agentID)
	if cur == "" {
		return false, "Agent 不在酒店中"
	}
	if cur == target {
		return false, "已经在目标位置"
	}
	if !w.space.CanMove(cur, target) {
		return false, "无法直接从 " + cur + " 移动到 " + target + "（没有连接）"
	}
	// 判定是 guest 还是 agent
	kind := "agent"
	evPrefix := "agent"
	if _, ok := w.guests[agentID]; ok {
		kind = "guest"
		evPrefix = "person"
	}
	// 触发离开 + 进入
	now := time.Now()
	w.bus.Publish(Event{Type: evPrefix + ".left", HotelID: w.space.HotelID(),
		LocationID: cur, SubjectID: agentID, SubjectKind: kind, Time: now})
	w.space.SetAgentLocation(agentID, target)
	if g, ok := w.guests[agentID]; ok {
		g.Location = target
	}
	if a, ok := w.agents[agentID]; ok {
		a.Location = target
	}
	w.bus.Publish(Event{Type: evPrefix + ".entered", HotelID: w.space.HotelID(),
		LocationID: target, SubjectID: agentID, SubjectKind: kind, Time: now})
	return true, "移动到 " + target
}

// Nearby 返回某位置附近的其他 Agent/Guest（同位置的）。
func (w *SpaceWorld) Nearby(locationID string) []NearbyEntity {
	ids := w.space.AgentsAt(locationID)
	out := make([]NearbyEntity, 0, len(ids))
	for _, id := range ids {
		out = append(out, NearbyEntity{
			AgentID:  id,
			Kind:     w.kindOf(id),
			Role:     w.roleOf(id),
			Distance: 0,
		})
	}
	return out
}

// NearbyEntity 附近对象。
type NearbyEntity struct {
	AgentID  int64  `json:"agent_id"`
	Kind     string `json:"kind"`
	Role     string `json:"role"`
	Distance float64 `json:"distance"`
}

// kindOf 返回 Agent/Guest 的 kind。
func (w *SpaceWorld) kindOf(id int64) string {
	if g, ok := w.guests[id]; ok {
		return g.Kind
	}
	if a, ok := w.agents[id]; ok {
		return a.Kind
	}
	return "unknown"
}

// roleOf 返回 Agent/Guest 的 role。
func (w *SpaceWorld) roleOf(id int64) string {
	if g, ok := w.guests[id]; ok {
		return g.Role
	}
	if a, ok := w.agents[id]; ok {
		return a.Role
	}
	return ""
}

// Perception 构建某 Agent 的空间感知（M8.1 十）。
// Agent 只看到：自己位置、附近对象、自己的负责区域、可到达位置。
func (w *SpaceWorld) Perception(agentID int64) map[string]interface{} {
	locID := w.space.AgentLocation(agentID)
	loc := w.space.Location(locID)
	perception := map[string]interface{}{
		"location": map[string]interface{}{
			"id": locID, "name": func() string {
				if loc != nil {
					return loc.Name
				}
				return locID
			}(),
		},
		"nearby":        w.Nearby(locID),
		"reachable":     w.space.Reachable(locID),
		"responsible":   []string{},
	}
	// 自己的责任区域
	if r := w.space.ResponsibilityOf(agentID); r != nil {
		perception["responsible"] = []string{r.Location}
	}
	return perception
}
