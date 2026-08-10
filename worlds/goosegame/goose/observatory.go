// Observatory —— AI 社会观察台的事件总线 + In-memory Event Store（M5 v0.1）。
//
// 职责：
//   - 收集游戏世界产生的所有事件（Agent 移动/任务/尸体/发言/投票/结束）。
//   - 维护最近 N 条事件的环形缓冲（默认 1000 条），供前端轮询/回看。
//   - 广播事件给实时 SSE 订阅者，前端据此驱动地图/Timeline 实时更新。
//
// 注意（M5 v0.1 范围）：
//   - 不做数据库持久化（Replay 第二版再做）。
//   - 只保留 In-memory 事件，进程重启即丢失。
//   - 并发安全：GameState 多 goroutine 写事件，Observatory 内部加锁。
package goose

import (
	"encoding/json"
	"sync"
	"time"
)

// ObsEvent 一条观察事件（wire 格式，SSE 的 data）。
type ObsEvent struct {
	Type string      `json:"type"` // agent.moved / task.completed / ...
	Time int64       `json:"time"` // 毫秒时间戳
	Data interface{} `json:"data"` // 事件体
}

// ObservOpts 观察台配置。
type ObservOpts struct {
	// MaxEvents 内存事件存储上限（默认 1000）。
	MaxEvents int
}

// Observatory 事件总线 + In-memory Event Store。
type Observatory struct {
	mu       sync.Mutex
	events   []ObsEvent     // 环形缓冲（最近 MaxEvents 条）
	max      int
	subs     map[int]chan ObsEvent // SSE 订阅者
	nextSub  int
}

// NewObservatory 创建观察台。
func NewObservatory(opts ObservOpts) *Observatory {
	max := opts.MaxEvents
	if max <= 0 {
		max = 1000
	}
	return &Observatory{
		max:   max,
		subs:  map[int]chan ObsEvent{},
	}
}

// Publish 发布一条事件：写入 Event Store + 广播给所有 SSE 订阅者。
func (o *Observatory) Publish(typ string, data interface{}) {
	ev := ObsEvent{Type: typ, Time: time.Now().UnixMilli(), Data: data}
	o.mu.Lock()
	o.events = append(o.events, ev)
	if len(o.events) > o.max {
		o.events = o.events[len(o.events)-o.max:]
	}
	// 广播给订阅者（非阻塞，缓冲满则丢弃，避免慢消费者拖垮游戏）
	for _, ch := range o.subs {
		select {
		case ch <- ev:
		default:
		}
	}
	o.mu.Unlock()
}

// Subscribe 注册一个 SSE 订阅者，返回一个接收事件的 channel 与取消函数。
func (o *Observatory) Subscribe(buffer int) (int, chan ObsEvent, func()) {
	if buffer <= 0 {
		buffer = 64
	}
	ch := make(chan ObsEvent, buffer)
	o.mu.Lock()
	id := o.nextSub
	o.nextSub++
	o.subs[id] = ch
	o.mu.Unlock()
	return id, ch, func() {
		o.mu.Lock()
		delete(o.subs, id)
		o.mu.Unlock()
	}
}

// RecentEvents 返回最近 n 条事件（供 GET /api/events）。
func (o *Observatory) RecentEvents(n int) []ObsEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.events) <= n {
		out := make([]ObsEvent, len(o.events))
		copy(out, o.events)
		return out
	}
	out := make([]ObsEvent, n)
	copy(out, o.events[len(o.events)-n:])
	return out
}

// Encode 把事件编码为 JSON 字节（SSE data 用）。
func (e ObsEvent) Encode() []byte {
	b, err := json.Marshal(e)
	if err != nil {
		return []byte(`{"type":"error","data":"marshal failed"}`)
	}
	return b
}
