// server.go —— Hotel World 的 HTTP 观测服务。
//
// API：
//	GET  /api/hotel             → 酒店信息
//	GET  /api/hotel/map         → 空间地图（位置 + 连接）
//	GET  /api/hotel/locations   → 全部位置
//	GET  /api/hotel/agents      → 酒店员工 + 客人（含位置/岗位/责任）
//	POST /api/hotel/actions/move → 移动 {location_id}
//	POST /api/hotel/events       → 模拟空间事件 {event, subject_id, location_id}
package hotel

import (
	"encoding/json"
	"net/http"
	"time"
)

// Server 酒店空间观测服务。
type Server struct {
	world *SpaceWorld
}

// NewServer 创建酒店观测服务。
func NewServer(world *SpaceWorld) *Server {
	return &Server{world: world}
}

// Mux 返回路由。
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/hotel", s.handleHotel)
	mux.HandleFunc("/api/hotel/map", s.handleMap)
	mux.HandleFunc("/api/hotel/locations", s.handleLocations)
	mux.HandleFunc("/api/hotel/agents", s.handleAgents)
	mux.HandleFunc("/api/hotel/actions/move", s.handleMove)
	mux.HandleFunc("/api/hotel/events", s.handleEvent)
	// M8.2：对话 / intent / 任务 / 入住
	mux.HandleFunc("/api/hotel/say", s.handleSay)
	mux.HandleFunc("/api/hotel/guest/say", s.handleGuestSay)
	mux.HandleFunc("/api/hotel/conversation", s.handleConversation)
	mux.HandleFunc("/api/hotel/tasks", s.handleTasks)
	mux.HandleFunc("/api/hotel/checkin", s.handleCheckIn)
	// M8.3：业务工具
	mux.HandleFunc("/api/hotel/run_checkin", s.handleRunCheckIn)
	mux.HandleFunc("/api/hotel/tool_logs", s.handleToolLogs)
	// 地图观察台（内嵌 map.html）
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(mapHTML)
	})
	return mux
}

// handleHotel GET /api/hotel —— 酒店信息。
func (s *Server) handleHotel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"id": s.world.Space().HotelID(), "name": s.world.Space().Name(),
		"locations": len(s.world.Space().Locations()),
	})
}

// handleMap GET /api/hotel/map —— 空间地图（位置 + 连接 + 员工 + 客人）。
func (s *Server) handleMap(w http.ResponseWriter, r *http.Request) {
	locs := s.world.Space().Locations()
	agents := s.world.Agents()
	guests := s.world.Guests()
	// 收集连接（去重）
	conns := []map[string]string{}
	seen := map[string]bool{}
	for _, l := range locs {
		for _, dest := range s.world.Space().Reachable(l.ID) {
			key := l.ID + "|" + dest
			if seen[key] || seen[dest+"|"+l.ID] {
				continue
			}
			seen[key] = true
			conns = append(conns, map[string]string{"from": l.ID, "to": dest})
		}
	}
	writeJSON(w, map[string]interface{}{
		"locations": locs, "agents": agents, "guests": guests, "connections": conns,
	})
}

// handleLocations GET /api/hotel/locations —— 全部位置。
func (s *Server) handleLocations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.world.Space().Locations())
}

// agentView 员工 + 客人的统一视图。
type agentView struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Location string `json:"location"`
	Busy     bool   `json:"busy"`
}

// handleAgents GET /api/hotel/agents —— 员工 + 客人。
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	views := []agentView{}
	for id, a := range s.world.Agents() {
		views = append(views, agentView{ID: id, Kind: a.Kind, Name: a.Name,
			Role: a.Role, Location: a.Location, Busy: s.world.isBusy(id)})
	}
	for id, g := range s.world.Guests() {
		views = append(views, agentView{ID: id, Kind: g.Kind, Name: g.Name,
			Role: g.Role, Location: g.Location})
	}
	writeJSON(w, views)
}

// moveReq 移动请求体。
type moveReq struct {
	AgentID    int64  `json:"agent_id"`
	LocationID string `json:"location_id"`
}

// handleMove POST /api/hotel/actions/move —— 移动。
func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req moveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ok, msg := s.world.MoveTo(req.AgentID, req.LocationID)
	writeJSON(w, map[string]interface{}{"success": ok, "message": msg})
}

// eventReq 模拟事件请求体。
type eventReq struct {
	Event      string `json:"event"`
	SubjectID  int64  `json:"subject_id"`
	LocationID string `json:"location_id"`
}

// handleEvent POST /api/hotel/events —— 模拟空间事件。
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req eventReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	responsible := int64(0)
	switch req.Event {
	case "person.entered":
		// 模拟一个 Guest 进入
		g := &Guest{ID: req.SubjectID, Kind: "human", Role: "guest", Name: "Guest", Location: req.LocationID}
		responsible = s.world.AddGuest(g)
	case "person.left", "person.moved", "agent.entered", "agent.left":
		s.world.Events().Publish(Event{Type: req.Event, HotelID: s.world.Space().HotelID(),
			LocationID: req.LocationID, SubjectID: req.SubjectID, Time: time.Now()})
	}
	writeJSON(w, map[string]interface{}{
		"event": req.Event, "responsible_agent": responsible,
	})
}

// ---- M8.2 对话 / intent / 任务 ----

// sayReq Agent 说话请求。
type sayReq struct {
	AgentID int64  `json:"agent_id"`
	Text    string `json:"text"`
}

// handleSay POST /api/hotel/say —— 让 Agent 说话。
func (s *Server) handleSay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req sayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	msg := s.world.Say(req.AgentID, req.Text)
	writeJSON(w, map[string]interface{}{"success": true, "message": msg})
}

// guestSayReq Guest 说话请求。
type guestSayReq struct {
	GuestID int64  `json:"guest_id"`
	Text    string `json:"text"`
}

// handleGuestSay POST /api/hotel/guest/say —— Guest 说话（解析 intent + 触发处理/交接）。
func (s *Server) handleGuestSay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req guestSayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	intent, msg := s.world.GuestSay(req.GuestID, req.Text)
	handler, handoff := s.world.HandleIntent(req.GuestID, intent)
	writeJSON(w, map[string]interface{}{
		"success": true, "intent": intent, "message": msg,
		"handler": handler, "handoff": handoff,
	})
}

// handleConversation GET /api/hotel/conversation —— 对话历史。
func (s *Server) handleConversation(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.world.Conversation())
}

// handleTasks GET /api/hotel/tasks —— 任务列表。
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.world.Tasks())
}

// checkInReq 模拟入住请求。
type checkInReq struct {
	GuestID   int64  `json:"guest_id"`
	AgentID   int64  `json:"agent_id"`
	GuestName string `json:"guest_name"`
}

// handleCheckIn POST /api/hotel/checkin —— 模拟入住（不接 PMS）。
func (s *Server) handleCheckIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req checkInReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	task, room, ok := s.world.CheckIn(req.GuestID, req.AgentID, req.GuestName)
	writeJSON(w, map[string]interface{}{
		"success": ok, "task": task.ID, "room": room,
		"guest_name": req.GuestName, "status": "checked_in",
	})
}

// runCheckInReq M8.3 业务入住请求。
type runCheckInReq struct {
	GuestID int64  `json:"guest_id"`
	AgentID int64  `json:"agent_id"`
}

// handleRunCheckIn POST /api/hotel/run_checkin —— 让 FrontDesk Planner 自主完成入住。
func (s *Server) handleRunCheckIn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req runCheckInReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ok, room, results := s.world.RunCheckInFlow(req.GuestID, IntentCheckIn)
	writeJSON(w, map[string]interface{}{
		"success": ok, "room": room, "steps": results,
	})
}

// handleToolLogs GET /api/hotel/tool_logs —— 工具调用记录（Audit/Timeline）。
func (s *Server) handleToolLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.world.Tools().Logs())
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}
