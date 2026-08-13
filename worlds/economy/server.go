// server.go —— Economy World 的 HTTP + SSE 观测服务（复用 goosegame 的观察台模式）。
//
// API：
//	GET /api/game          → 经济世界快照（Agent 资产/价格/开放工作/最近交易/总财富）
//	GET /api/agents/{id}   → 单个 Agent 的深度经济状态（Inspector）
//	GET /api/events        → 最近事件（In-memory）
//	GET /api/events/stream → SSE 实时事件流
//
// 前端：若存在 webstatic/dist 的内嵌产物（go:embed），则非 /api 路径直接提供前端页面，
// 使整个世界可单文件部署（exe 或容器）。未构建前端时不影响 API。
package economy

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"agentworld/worlds/economy/webstatic"
)

// Server 经济世界观测服务。
type Server struct {
	mod *Module
	mux *http.ServeMux
}

// NewServer 创建观测服务。
func NewServer(mod *Module) *Server {
	s := &Server{mod: mod, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/game", s.handleGame)
	s.mux.HandleFunc("/api/agents/", s.handleAgent)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	s.mux.HandleFunc("/api/events/stream", s.handleStream)
	// M7 Human Entrance
	s.mux.HandleFunc("/api/auth/register", s.handleRegister)
	s.mux.HandleFunc("/api/auth/login", s.handleLogin)
	s.mux.HandleFunc("/api/world", s.handleWorld)
	s.mux.HandleFunc("/api/actions/do_job", s.handleHumanDoJob)
	s.mux.HandleFunc("/api/actions/buy_skill", s.handleHumanBuySkill)
	s.mux.HandleFunc("/api/actions/hire_agent", s.handleHumanHireAgent)
	s.mux.HandleFunc("/", s.serveWeb) // 内嵌前端（非 /api 回退 index.html）
	return s
}

// webFS 从内嵌前端（webstatic.DistFS 的 dist 子目录）提供静态资源。
var webFS = func() fs.FS {
	sub, err := fs.Sub(webstatic.DistFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}()

// serveWeb 提供前端静态资源；非 /api 路径回退到 index.html（SPA 路由）。
func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "." || p == "\\" {
		p = "index.html"
	}
	if data, err := fs.ReadFile(webFS, p); err == nil {
		w.Header().Set("Content-Type", contentType(p))
		_, _ = w.Write(data)
		return
	}
	// 资源不存在：回退 index.html（若连 index.html 都没有，说明前端未构建）
	if index, err := fs.ReadFile(webFS, "index.html"); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
		return
	}
	http.NotFound(w, r)
}

// contentType 根据扩展名返回 MIME，避免 fs.ReadFile 在 Windows 下缺类型。
func contentType(p string) string {
	switch path.Ext(p) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	default:
		return "application/octet-stream"
	}
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) Start(addr string) error {
	log.Printf("[economy] 经济观察台监听 %s", addr)
	srv := &http.Server{Addr: addr, Handler: s.mux}
	return srv.ListenAndServe()
}

func (s *Server) handleGame(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mod.Game().Snapshot())
}

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/agents/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "bad agent id", http.StatusBadRequest)
		return
	}
	ins := s.mod.Game().Inspector(id)
	if ins == nil {
		http.Error(w, "agent not found", http.StatusNotFound)
		return
	}
	writeJSON(w, ins)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mod.Game().Observatory().RecentEvents(200))
}

// handleStream SSE 实时事件流（复用 goosegame 的格式：data: {json}\n\n）。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher.Flush()

	_, ch, cancel := s.mod.Game().Observatory().Subscribe(64)
	defer cancel()
	if ch == nil {
		// 订阅者已达上限：拒绝连接，提示稍后重试
		http.Error(w, "subscription limit reached, retry later", http.StatusServiceUnavailable)
		return
	}
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", ev.Encode())
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// ---- M7 Human Entrance ----

// authReq 注册/登录请求体。
type authReq struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleRegister POST /api/auth/register —— 注册 Human Agent。
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req authReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id, token, code := s.mod.Game().RegisterHuman(req.Name, req.Password)
	if code != "ok" {
		// 返回具体原因，前端提示（limit = 注册上限已满）
		status := http.StatusConflict
		if code == "limit" {
			status = http.StatusForbidden // 403：注册已满，防攻击
		}
		writeJSON(w, map[string]interface{}{"code": code, "error": registerErrMsg(code)})
		w.WriteHeader(status)
		return
	}
	writeJSON(w, map[string]interface{}{
		"token": token, "agent_id": id, "name": req.Name, "kind": "human",
		"balance": 100, "max_humans": s.mod.MaxHumans(),
	})
}

// registerErrMsg 注册失败原因的中文提示。
func registerErrMsg(code string) string {
	switch code {
	case "limit":
		return "注册用户已达上限，暂时无法注册"
	case "duplicate":
		return "该用户名已被注册"
	case "invalid":
		return "用户名不能为空，密码至少 4 位"
	default:
		return "注册失败"
	}
}

// handleLogin POST /api/auth/login —— Human 登录。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req authReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id, token, ok := s.mod.Game().LoginHuman(req.Name, req.Password)
	if !ok {
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]interface{}{
		"token": token, "agent_id": id, "name": req.Name, "kind": "human",
	})
}

// handleWorld GET /api/world —— Human 视角的世界状态（含自己的 Agent + 行动入口）。
// 若带有效 token，附带"我的 Agent"信息；否则只返回世界公开状态。
func (s *Server) handleWorld(w http.ResponseWriter, r *http.Request) {
	snap := s.mod.Game().Snapshot()
	me := map[string]interface{}{}
	if id, ok := s.authFromRequest(r); ok {
		if a := s.mod.Game().AgentOf(id); a != nil {
			me = map[string]interface{}{
				"id": a.ID, "name": a.Name, "kind": a.Kind, "profession": a.Profession,
				"balance": a.Balance, "reputation": a.Reputation, "skills": a.Skills,
				"completed": a.CompletedContracts, "failed": a.FailedContracts,
				"totalEarned": a.TotalEarned, "totalSpent": a.TotalSpent,
			}
		}
	}
	writeJSON(w, map[string]interface{}{
		"round": snap.Round, "agents": snap.Agents, "totalWealth": snap.TotalWealth,
		"openJobs": snap.OpenJobs, "skillMarket": snap.SkillMarket,
		"services": snap.Services, "contracts": snap.Contracts,
		"contractStats": snap.ContractStats, "me": me,
	})
}

// actionReq 通用行动请求体。
type actionReq struct {
	JobID     int64  `json:"job_id"`
	SkillID   string `json:"skill_id"`
	ServiceID string `json:"service_id"`
	WorkerID  int64  `json:"worker_id"`
}

// handleHumanDoJob POST /api/actions/do_job —— Human 执行工作。
func (s *Server) handleHumanDoJob(w http.ResponseWriter, r *http.Request) {
	agentID, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req actionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	reward, msg, success := s.mod.Game().HumanDoJob(agentID, req.JobID)
	writeJSON(w, map[string]interface{}{"success": success, "reward": reward, "message": msg})
}

// handleHumanBuySkill POST /api/actions/buy_skill —— Human 购买技能。
func (s *Server) handleHumanBuySkill(w http.ResponseWriter, r *http.Request) {
	agentID, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req actionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	success, msg := s.mod.Game().HumanBuySkill(agentID, req.SkillID)
	writeJSON(w, map[string]interface{}{"success": success, "message": msg})
}

// handleHumanHireAgent POST /api/actions/hire_agent —— Human 雇佣 AI。
func (s *Server) handleHumanHireAgent(w http.ResponseWriter, r *http.Request) {
	agentID, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	var req actionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, success, msg := s.mod.Game().HumanHireAgent(agentID, req.WorkerID, req.ServiceID)
	writeJSON(w, map[string]interface{}{"success": success, "message": msg})
}

// authFromRequest 从请求头解析 Bearer token → agentID。
func (s *Server) authFromRequest(r *http.Request) (int64, bool) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return s.mod.Game().AuthHuman(h[7:])
	}
	return 0, false
}

// requireAuth 需要鉴权的 handler 包装：无有效 token 返回 401。
func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, ok := s.authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}
