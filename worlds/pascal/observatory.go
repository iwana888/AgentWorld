package pascal

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Observatory 记录 Agent 在 Pascal World 里的轨迹：状态 / Intent / Memory / Timeline。
// 它消费 Runtime 产出的事实（Context Tokens、Retrieved Memory、工具调用），
// 不修改 Runtime 本身。
type Observatory struct {
	mu      sync.Mutex
	agent   string
	events  []TimelineEvent
	current map[string]string // 实时状态快照
	subs    []chan TimelineEvent
}

// NewObservatory 构造 Observatory。
func NewObservatory(agent string) *Observatory {
	return &Observatory{
		agent:   agent,
		current: map[string]string{"agent": agent, "status": "idle"},
	}
}

// Publish 记录一条轨迹事件并广播给 SSE 订阅者。
func (o *Observatory) Publish(e TimelineEvent) {
	o.mu.Lock()
	o.events = append(o.events, e)
	o.current["issue"] = e.IssueID
	o.current["step"] = e.Step
	o.current["status"] = map[bool]string{true: "ok", false: "fail"}[e.OK]
	subs := append([]chan TimelineEvent{}, o.subs...)
	o.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Recent 返回最近 n 条事件（供 GET /api/events）。
func (o *Observatory) Recent(n int) []TimelineEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	if n <= 0 || n > len(o.events) {
		n = len(o.events)
	}
	out := make([]TimelineEvent, n)
	copy(out, o.events[len(o.events)-n:])
	return out
}

// Snapshot 返回当前状态快照。
func (o *Observatory) Snapshot() map[string]interface{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := map[string]interface{}{
		"agent":  o.agent,
		"status": o.current["status"],
		"issue":  o.current["issue"],
		"step":   o.current["step"],
		"events": o.Recent(50),
	}
	return s
}

// Subscribe 注册一个 SSE 订阅者。
func (o *Observatory) Subscribe() (chan TimelineEvent, func()) {
	o.mu.Lock()
	ch := make(chan TimelineEvent, 64)
	o.subs = append(o.subs, ch)
	o.mu.Unlock()
	return ch, func() {
		o.mu.Lock()
		for i, c := range o.subs {
			if c == ch {
				o.subs = append(o.subs[:i], o.subs[i+1:]...)
				break
			}
		}
		o.mu.Unlock()
	}
}

// SSEHandler 返回 SSE 事件流 handler。
func (o *Observatory) SSEHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		ch, cancel := o.Subscribe()
		defer cancel()
		for {
			select {
			case <-r.Context().Done():
				return
			case e := <-ch:
				b, _ := json.Marshal(e)
				_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
				flusher.Flush()
			case <-time.After(30 * time.Second):
				_, _ = w.Write([]byte(": keep-alive\n\n"))
				flusher.Flush()
			}
		}
	}
}

// SnapshotHandler 返回当前状态 JSON。
func (o *Observatory) SnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(o.Snapshot())
	}
}
