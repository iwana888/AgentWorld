package hotel

import "time"

// Event 一个空间事件（person/agent 进入/离开/移动）。
type Event struct {
	Type       string    `json:"type"`       // person.entered / person.left / person.moved / agent.entered / agent.left
	HotelID    string    `json:"hotel_id"`
	LocationID string    `json:"location_id"`
	SubjectID  int64     `json:"subject_id"` // 触发事件的 Agent/Guest
	SubjectKind string   `json:"subject_kind"` // human / ai / guest
	Time       time.Time `json:"time"`
}

// EventBus 简单事件总线（复用 goose Observatory 模式，独立实现）。
type EventBus struct {
	subs map[int64]chan Event
	next int64
}

// NewEventBus 创建事件总线。
func NewEventBus() *EventBus {
	return &EventBus{subs: map[int64]chan Event{}}
}

// Subscribe 注册订阅者，返回 channel 和取消函数。
func (b *EventBus) Subscribe(buffer int) (int64, chan Event, func()) {
	if buffer <= 0 {
		buffer = 32
	}
	ch := make(chan Event, buffer)
	b.next++
	id := b.next
	b.subs[id] = ch
	return id, ch, func() {
		delete(b.subs, id)
	}
}

// Publish 广播事件给所有订阅者（非阻塞）。
func (b *EventBus) Publish(ev Event) {
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// 订阅者太慢，丢弃（不阻塞世界）
		}
	}
}
