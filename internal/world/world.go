// Package world 提供 World Engine（M6）：让世界主动变化，而非只有 Agent 驱动世界。
//
// 职责：
//   - Time：维护虚拟时间（真实时间加速，跨天触发"新的一天"）
//   - Environment：随机天气变化，生成天气事件
//   - Resource：热度指数波动（技术/财经/生活），生成市场/热点事件
//   - Event：世界级事件写入 world_events 表，供所有 Agent 感知
//
// World Engine 不知道任何具体世界（社交/酒店），只负责"世界在运转"。
// 具体事件如何影响 Agent，由各 Module 决定。
package world

import (
	"math/rand"
	"time"

	"agentworld/internal/models"

	"gorm.io/gorm"
)

// Event 世界级事件（Agent 之外的世界变化）。
type Event struct {
	Type      string
	Title     string
	Detail    string
	TargetTag string // 影响分类（tech/finance/life...），空=所有
	CreatedAt time.Time
}

// Engine World Engine：持有 DB，周期性 Tick。
type Engine struct {
	db       *gorm.DB
	interval time.Duration // tick 间隔（现实时间）
	timeMult int           // 时间加速倍率：1 现实秒 = timeMult 虚拟秒
}

// NewEngine 构造 World Engine。interval 为 tick 间隔，timeMult 为时间加速倍率。
func NewEngine(d *gorm.DB, interval time.Duration, timeMult int) *Engine {
	if timeMult <= 0 {
		timeMult = 60 // 默认 1 秒 = 1 分钟虚拟时间
	}
	return &Engine{db: d, interval: interval, timeMult: timeMult}
}

// Start 启动后台 tick 循环。返回停止 channel。
func (e *Engine) Start() chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(e.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.Tick()
			case <-stop:
				return
			}
		}
	}()
	return stop
}

// Tick 推进世界一步：时间 + 天气 + 热度。
func (e *Engine) Tick() {
	e.tickTime()
	e.tickWeather()
	e.tickHeat()
}

// Events 返回自 since 以来的世界事件。
func (e *Engine) Events(since time.Time) []Event {
	var we []models.WorldEvent
	if err := e.db.Where("created_at >= ?", since).Order("id DESC").Limit(20).Find(&we).Error; err != nil {
		return nil
	}
	var out []Event
	for _, w := range we {
		out = append(out, Event{Type: w.Type, Title: w.Title, Detail: w.Detail, TargetTag: w.TargetTag, CreatedAt: w.CreatedAt})
	}
	return out
}

// 天气池：事件化，变化时产生 WorldEvent。
var weathers = []struct {
	name string
	icon string
	tag  string
}{
	{"晴天", "☀️", "life"},
	{"多云", "⛅", "life"},
	{"小雨", "🌧️", "travel"},
	{"大雨", "🌧️", "travel"},
	{"暴雨", "⛈️", "travel"},
	{"寒潮", "❄️", "life"},
}

// tickTime 推进虚拟时间，跨天触发"新的一天"事件。
func (e *Engine) tickTime() {
	// 读取/初始化虚拟时间
	virtual, _ := e.getState("world_time")
	now := time.Now()
	if virtual == "" {
		e.setState("world_time", now.Format("2006-01-02 15:04"))
		return
	}
	t, err := time.Parse("2006-01-02 15:04", virtual)
	if err != nil {
		e.setState("world_time", now.Format("2006-01-02 15:04"))
		return
	}
	// 推进 timeMult 秒（按现实间隔估算：interval 秒 = timeMult 虚拟秒）
	advance := time.Duration(e.interval.Seconds()*float64(e.timeMult)) * time.Second
	newT := t.Add(advance)
	if newT.Day() != t.Day() {
		// 跨天：新的一天
		e.setState("world_time", newT.Format("2006-01-02 15:04"))
		e.AddEvent(Event{
			Type: "day", Title: "新的一天", TargetTag: "",
			Detail: "新的一天开始了，世界重新焕发活力。",
		})
		return
	}
	e.setState("world_time", newT.Format("2006-01-02 15:04"))
}

// tickWeather 随机天气变化（小概率），变化时产生事件。
func (e *Engine) tickWeather() {
	cur, _ := e.getState("weather")
	if rand.Float64() < 0.05 { // 5% 概率天气变化
		w := weathers[rand.Intn(len(weathers))]
		if w.name != cur {
			e.setState("weather", w.name)
			e.AddEvent(Event{
				Type: "weather", Title: "天气：" + w.icon + w.name, TargetTag: w.tag,
				Detail: "天气变成了" + w.name + "，可能会影响出行与安排。",
			})
		}
	}
}

// 热度话题池：随机一个话题热度暴涨，生成市场/热点事件。
var heatTopics = []struct {
	tag    string
	title  string
	detail string
}{
	{"tech", "大模型新突破", "有公司发布了新一代大模型，技术圈热议。"},
	{"tech", "AI 编程工具刷屏", "某 AI 编程工具爆火，程序员圈都在讨论。"},
	{"finance", "股市大涨", "A股放量上涨，股民情绪高涨。"},
	{"finance", "央行降息", "央行宣布降息，投资圈炸开锅。"},
	{"life", "爆款美食走红", "某家餐厅突然排队爆满，成为打卡热点。"},
	{"life", "城市音乐节", "周末城市音乐节，年轻人扎堆。"},
	{"tech", "芯片新突破", "国产芯片传来新消息，科技媒体集中报道。"},
	{"finance", "房价信号", "某城市楼市出现新动向，引发讨论。"},
}

// tickHeat 随机触发一个热点话题（低概率）。
func (e *Engine) tickHeat() {
	if rand.Float64() < 0.08 { // 8% 概率出现新热点
		t := heatTopics[rand.Intn(len(heatTopics))]
		e.AddEvent(Event{
			Type: "market", Title: "热议：" + t.title, TargetTag: t.tag,
			Detail: t.detail,
		})
	}
}

// getState 读取世界状态值。
func (e *Engine) getState(key string) (string, error) {
	var s models.WorldState
	err := e.db.Where("key = ?", key).First(&s).Error
	if err != nil {
		return "", err
	}
	return s.Value, nil
}

// setState 写入世界状态。
func (e *Engine) setState(key, value string) error {
	var s models.WorldState
	err := e.db.Where("key = ?", key).First(&s).Error
	if err == nil {
		return e.db.Model(&models.WorldState{}).Where("id = ?", s.ID).Updates(map[string]any{
			"value": value, "updated_at": time.Now(),
		}).Error
	}
	return e.db.Create(&models.WorldState{Key: key, Value: value, UpdatedAt: time.Now()}).Error
}

// AddEvent 写入一条世界事件。
func (e *Engine) AddEvent(ev Event) error {
	return e.db.Create(&models.WorldEvent{
		Type: ev.Type, Title: ev.Title, Detail: ev.Detail,
		TargetTag: ev.TargetTag, CreatedAt: time.Now(),
	}).Error
}
