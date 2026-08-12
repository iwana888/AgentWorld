// server.go —— Economy World 的 HTTP + SSE 观测服务（复用 goosegame 的观察台模式）。
//
// API：
//	GET /api/game          → 经济世界快照（Agent 资产/价格/开放工作/最近交易/总财富）
//	GET /api/agents/{id}   → 单个 Agent 的深度经济状态（Inspector）
//	GET /api/events        → 最近事件（In-memory）
//	GET /api/events/stream → SSE 实时事件流
package economy

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
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
	return s
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

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}
