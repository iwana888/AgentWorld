// GooseGame 的 HTTP + SSE 观测服务（AI 社会观察台，M5 v0.1）。
//
// 定位：这是 GooseGame 世界自己的展示入口，不复用 AgentWorld 主框架的 web。
// 独立世界（worlds/werewolf、worlds/business 等）也可以有各自的展示入口。
// 只复用 Runtime/Event/World State 能力，HTTP/SSE Adapter 属于 GooseGame 自己。
//
// API（M5 v0.1 极简）：
//	GET /api/game              → 当前游戏状态（阶段/回合/存活 Agent 位置/尸体）
//	GET /api/agents            → Agent 公共信息（名字/位置/存活/身份）
//	GET /api/events            → 最近事件（In-memory）
//	GET /api/events/stream     → SSE 实时事件流
//
// 注意：Agent 的主观状态（Belief/Relationship）不通过普通 API 暴露——
// 它们对每个 Agent 私有，避免破坏信息隔离。
package goosegame

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
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
	s.mux.HandleFunc("/api/events", s.handleEvents)
	s.mux.HandleFunc("/api/events/stream", s.handleStream)
	return s
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
