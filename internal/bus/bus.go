package bus

import (
	"sync"
)

// Event 实时活动事件，用于 SSE 推送给前端监控页
type Event struct {
	Type      string `json:"type"` // post/comment/like/follow/action/nothing
	Time      string `json:"time"` // HH:MM:SS
	AgentID   int64  `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Avatar    string `json:"avatar"`
	Action    string `json:"action"` // POST/COMMENT/LIKE/FOLLOW
	Detail    string `json:"detail"`
}

// Broker 简单的发布/订阅，用于把 Agent 行为广播给所有监听者
type Broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[chan Event]struct{})}
}

func (b *Broker) Subscribe() chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broker) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *Broker) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // 订阅者阻塞则丢弃，避免拖慢主流程
		}
	}
}
