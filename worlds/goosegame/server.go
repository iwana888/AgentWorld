// GooseGame 的 HTTP + SSE 观测服务（AI 社会观察台，M5 v0.1）。
//
// 定位：这是 GooseGame 世界自己的展示入口，不复用 AgentWorld 主框架的 web。
// 独立世界（worlds/werewolf、worlds/business 等）也可以有各自的展示入口。
// 只复用 Runtime/Event/World State 能力，HTTP/SSE Adapter 属于 GooseGame 自己。
//
// API（M5 v0.1 极简）：
//	GET /api/game              → 当前游戏状态（阶段/回合/存活 Agent 位置/尸体）
//	GET /api/agents            → Agent 公共信息（名字/位置/存活/身份）
//	GET /api/agents/{id}       → 单个 Agent 的深度私有状态（Agent Inspector）
//	GET /api/events            → 最近事件（In-memory）
//	GET /api/events/stream     → SSE 实时事件流
//
// 注意：Agent 的主观状态（Belief/Relationship）不通过公开的 /api/game、/api/agents
// 暴露——它们对每个 Agent 私有。只有点击某 Agent 时按需请求 /api/agents/{id}
// （Agent Inspector）才返回该 Agent 自己的主观状态，面向调试。
//
// 前端：若存在 webstatic/dist 的内嵌产物（go:embed），则非 /api 路径直接提供前端页面，
// 使整个世界可单文件部署（exe 或容器）。未构建前端时不影响 API。
package goosegame

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

	"agentworld/worlds/goosegame/webstatic"
)

// Server 鸭鹅杀观测服务。
type Server struct {
	mod *GooseModule
	mux *http.ServeMux
}

// NewServer 创建观测服务，注册所有路由。
func NewServer(mod *GooseModule) *Server {
	s := &Server{mod: mod, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/game", s.handleGame)
	s.mux.HandleFunc("/api/agents", s.handleAgents)
	s.mux.HandleFunc("/api/agents/", s.handleAgentInspector)
	s.mux.HandleFunc("/api/events", s.handleEvents)
	s.mux.HandleFunc("/api/events/stream", s.handleStream)
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
		w.Header().Set("Content-Type", webContentType(p))
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

// webContentType 根据扩展名返回 MIME，避免 fs.ReadFile 在 Windows 下缺类型。
func webContentType(p string) string {
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

// Handler 返回 http.Handler（供 net/http 使用）。
func (s *Server) Handler() http.Handler { return s.mux }

// Start 启动 HTTP 服务（阻塞）。addr 如 ":19090"。
func (s *Server) Start(addr string) error {
	log.Printf("[observatory] AI 社会观察台监听 %s", addr)
	log.Printf("[observatory] 打开 http://localhost%s 查看 Agent 社会", addr)
	srv := &http.Server{Addr: addr, Handler: s.mux}
	return srv.ListenAndServe()
}

// ---- handlers ----

func (s *Server) handleGame(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mod.Game().Snapshot())
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mod.Game().PublicAgents())
}

// handleAgentInspector 返回单个 Agent 的深度私有状态（Agent Inspector，面向调试）。
// 路由：/api/agents/{id}
func (s *Server) handleAgentInspector(w http.ResponseWriter, r *http.Request) {
	// 去掉 "/api/agents/" 前缀
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
	writeJSON(w, s.mod.Observatory().RecentEvents(200))
}

// handleStream SSE 实时事件流。
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

	_, ch, cancel := s.mod.Observatory().Subscribe(64)
	defer cancel()

	// 心跳，保持连接
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			// SSE 格式：data: {json}\n\n（不命名 event，保证 EventSource 的 onmessage 能收到；
			// 事件类型已包含在 data 的 type 字段里）
			fmt.Fprintf(w, "data: %s\n\n", ev.Encode())
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}
