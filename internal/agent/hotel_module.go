// hotel_module.go — HotelModule：第二个完全不同的世界，验证 Runtime 通用性。
//
// 世界：一家酒店。Agent 扮演不同角色（前台/客房/工程/营收），围绕房间状态与
// 预订协作。动作与社交完全不同（checkin/checkout/clean/maintain/review/nothing），
// 但通过同一个 Module 接口被 Runtime 驱动——证明 Runtime 不知道世界是什么也能跑。
package agent

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"agentworld/internal/bus"
	"agentworld/internal/db"
	"agentworld/internal/llm"
	"agentworld/internal/logx"
	"agentworld/internal/models"
	"agentworld/sdk"
)

// hotelPerception 酒店场景的结构化感知。
type hotelPerception struct {
	prompt      string          // LLM 提示词
	rooms       []models.HotelRoom // 当前可见房间
	booking     models.HotelBooking // 该 Agent 的活跃预订（若有）
}

// HotelModule 酒店世界模块。
// M11：官方 Module 不再依赖 *Runtime，只通过 sdk.Runtime 上下文访问能力。
type HotelModule struct {
	rt  sdk.Runtime
	llm *llm.Client
}

// NewHotelModule 构造酒店模块。
func NewHotelModule(rt sdk.Runtime, llmClient *llm.Client) *HotelModule {
	return &HotelModule{rt: rt, llm: llmClient}
}

func (m *HotelModule) Name() string { return "hotel" }

// OnBoot 初始化世界：幂等填充默认房间。
func (m *HotelModule) OnBoot(rt sdk.Runtime) error {
	n, err := db.SeedHotelRooms(rt.DB())
	if err != nil {
		return err
	}
	logx.I("hotel world boot", logx.F{"rooms": n})
	return nil
}

// Perceive 构建某 Agent 的酒店视图：当前房间状态 + 自己的活跃预订。
func (m *HotelModule) Perceive(ctx context.Context, a sdk.Agent) (sdk.Perception, error) {
	ma := fromSDKAgent(a)
	rooms, _ := db.ListRooms(m.rt.DB(), "")
	booking, _ := db.ActiveBookingByAgent(m.rt.DB(), ma.ID)
	return &hotelPerception{
		prompt:  m.buildPrompt(ma, rooms, booking),
		rooms:   rooms,
		booking: booking,
	}, nil
}

func (m *HotelModule) Planner() Planner { return &HotelPlanner{rt: m.rt, llm: m.llm, mod: m} }
func (m *HotelModule) Executor() Executor { return &HotelExecutor{rt: m.rt, mod: m} }

// WakePolicy 复用事件驱动策略（有空房/待清洁等事件时唤醒）。
func (m *HotelModule) WakePolicy() WakePolicy {
	return NewEventWakePolicy(m.rt, 0.15)
}

// buildPrompt 酒店场景 LLM 提示词。
func (m *HotelModule) buildPrompt(a models.Agent, rooms []models.HotelRoom, booking models.HotelBooking) string {
	var b strings.Builder
	b.WriteString("当前时间：" + time.Now().Format("2006-01-02 15:04") + "\n")
	b.WriteString("你是一家酒店的" + a.Interests + "。当前房间状态：\n")
	for _, r := range rooms {
		b.WriteString(fmt.Sprintf("- %s 房（%s）%d元 状态=%s\n", r.Number, r.RoomType, r.Price, r.Status))
	}
	if booking.ID != 0 {
		b.WriteString("你当前入住 " + booking.RoomNumber + " 房。\n")
	} else {
		b.WriteString("你当前没有入住。\n")
	}
	b.WriteString("\n你的可选动作：checkin(入住空房)/checkout(退房)/clean(清洁)/maintain(维修)/review(评价)/nothing。\n")
	// M9：若已注册 PMS 能力，告知 Agent 可调用真实 PMS 工具（发卡/销卡/查卡）。
	for _, c := range m.rt.Capabilities() {
		if c.Name == "pms" && len(c.Tools) > 0 {
			b.WriteString("\n你还可以调用外部 PMS（酒店门锁房卡系统）工具，动作格式 tool:<工具名>，参数放入 tool_args JSON：\n")
			for _, t := range c.Tools {
				b.WriteString(fmt.Sprintf("- tool:%s（%s），参数：", t.Name, t.Description))
				for _, p := range t.Parameters {
					req := ""
					if p.Required {
						req = "必填"
					}
					b.WriteString(fmt.Sprintf("%s[%s %s] ", p.Name, p.Type, req))
				}
				b.WriteString("\n")
			}
			b.WriteString("例如前台为客人发房卡：action=tool:send_room_key, tool_args={\"roomNumber\":\"104\",\"lockNumber\":\"104\",\"cardType\":1,\"arrivalDate\":\"2026-08-07 14:00:00\",\"departureDate\":\"2026-08-08 12:00:00\",\"custom1\":\"客人姓名\"}\n")
		}
	}
	b.WriteString("请基于角色与当前状态，决定下一步，只返回 JSON。\n")
	return b.String()
}

// ---- Planner ----

type HotelPlanner struct {
	rt  sdk.Runtime
	llm *llm.Client
	mod *HotelModule
}

func (p *HotelPlanner) Decide(ctx context.Context, a sdk.Agent, perc sdk.Perception) (*sdk.Decision, error) {
	ma := fromSDKAgent(a)
	hp, _ := perc.(*hotelPerception)
	if hp == nil {
		return &sdk.Decision{Action: "nothing"}, nil
	}
	// M8：计划优先——有活跃计划则按当前步骤行动。
	if plan := p.mod.ensurePlan(ma); plan != nil {
		if step := currentStep(p.rt, plan); step != "" && isHotelAction(step) {
			dec := &llm.Decision{Action: step}
			if p.fillHotelStep(hp, dec) {
				advancePlan(p.rt, plan)
				return toSDKDecision(dec), nil
			}
		}
	}
	var dec *llm.Decision
	if p.rt.UseLLM(a) {
		if d, err := p.llm.Decide(ctx, a.SystemPrompt, hp.prompt); err == nil && d != nil && isHotelAction(d.Action) {
			dec = d
		}
	}
	if dec == nil {
		dec = p.mockDecide(ma, hp)
	}
	return toSDKDecision(dec), nil
}

// fillHotelStep 为酒店计划步骤补齐目标（房间/预订），返回是否可执行。
func (p *HotelPlanner) fillHotelStep(hp *hotelPerception, dec *llm.Decision) bool {
	switch dec.Action {
	case "checkin":
		for _, r := range hp.rooms {
			if r.Status == db.RoomAvailable {
				dec.Target = r.ID
				dec.TargetKind = "room_id"
				return true
			}
		}
	case "checkout":
		if hp.booking.ID != 0 {
			dec.Target = hp.booking.ID
			dec.TargetKind = "booking_id"
			return true
		}
	case "clean":
		for _, r := range hp.rooms {
			if r.Status == db.RoomCleaning {
				dec.Target = r.ID
				dec.TargetKind = "room_id"
				return true
			}
		}
	case "maintain":
		for _, r := range hp.rooms {
			if r.Status == db.RoomAvailable {
				dec.Target = r.ID
				dec.TargetKind = "room_id"
				return true
			}
		}
	case "review":
		return true
	}
	return false // 无可用目标，推进计划下次再试
}

// mockDecide 规则决策：根据角色（Interests）与房间状态。
func (p *HotelPlanner) mockDecide(a models.Agent, hp *hotelPerception) *llm.Decision {
	dec := &llm.Decision{Action: "nothing"}

	// 有活跃预订：优先退房
	if hp.booking.ID != 0 {
		dec.Action = "checkout"
		dec.Target = hp.booking.ID
		dec.TargetKind = "booking_id"
		dec.Reason = "入住结束，办理退房"
		return dec
	}

	var avail, cleaning, maintenance []models.HotelRoom
	for _, r := range hp.rooms {
		switch r.Status {
		case db.RoomAvailable:
			avail = append(avail, r)
		case db.RoomCleaning:
			cleaning = append(cleaning, r)
		case db.RoomMaintenance:
			maintenance = append(maintenance, r)
		}
	}

	// 按角色倾向决策
	in := a.Interests
	switch {
	case strings.Contains(in, "前台") && len(avail) > 0:
		r := avail[rand.Intn(len(avail))]
		// M9：若 PMS 能力可用，前台办理入住时调用真实 PMS 发卡（形成"Agent 连接现实"闭环）。
		if pmsCapability(p.rt) && rand.Float32() < 0.7 {
			dec.Action = "tool:send_room_key"
			dec.ToolArgs = map[string]interface{}{
				"roomNumber":    r.Number,
				"lockNumber":    r.Number,
				"cardType":      1,
				"arrivalDate":   time.Now().Format("2006-01-02 15:04:05"),
				"departureDate": time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05"),
				"custom1":       "客人",
			}
			dec.Reason = "办理入住并发放房卡"
			dec.Importance = 4
			return dec
		}
		dec.Action = "checkin"
		dec.Target = r.ID
		dec.TargetKind = "room_id"
		dec.Content = fmt.Sprintf("办理 %s 房入住", r.Number)
		dec.Reason = "有空房，为客人办理入住"
		dec.Memory = "给客人办了 " + r.Number + " 房入住"
		dec.MemoryType = "event"
	case strings.Contains(in, "客房") && len(cleaning) > 0:
		r := cleaning[rand.Intn(len(cleaning))]
		dec.Action = "clean"
		dec.Target = r.ID
		dec.TargetKind = "room_id"
		dec.Content = fmt.Sprintf("清洁 %s 房", r.Number)
		dec.Reason = "有退房待清洁"
	case strings.Contains(in, "工程") && len(avail) > 0 && rand.Float32() < 0.4:
		r := avail[rand.Intn(len(avail))]
		dec.Action = "maintain"
		dec.Target = r.ID
		dec.TargetKind = "room_id"
		dec.Content = fmt.Sprintf("检修 %s 房设备", r.Number)
		dec.Reason = "例行设备检修"
	case strings.Contains(in, "营收"):
		dec.Action = "review"
		dec.Reason = "评估今日入住情况，写运营点评"
		dec.Memory = "今天复盘了酒店入住数据"
		dec.MemoryType = "self"
	case len(cleaning) > 0:
		r := cleaning[rand.Intn(len(cleaning))]
		dec.Action = "clean"
		dec.Target = r.ID
		dec.TargetKind = "room_id"
		dec.Reason = "顺手协助清洁"
	}
	return dec
}

// pmsCapability 判断是否已注册 PMS 能力，且具备 send_room_key 工具。
// M11：只通过 sdk.Runtime.Capabilities() 访问，不接触 *Runtime 字段。
func pmsCapability(rt sdk.Runtime) bool {
	if rt == nil {
		return false
	}
	for _, c := range rt.Capabilities() {
		if c.Name == "pms" {
			for _, t := range c.Tools {
				if t.Name == "send_room_key" {
					return true
				}
			}
		}
	}
	return false
}

func isHotelAction(s string) bool {
	switch s {
	case "checkin", "checkout", "clean", "maintain", "review", "nothing":
		return true
	}
	// M9：放行外部工具调用动作（tool:send_room_key 等），由 Runtime 路由到 Capability。
	return strings.HasPrefix(s, "tool:")
}

// ---- Executor ----

type HotelExecutor struct {
	rt  sdk.Runtime
	mod *HotelModule
}

func (e *HotelExecutor) Execute(ctx context.Context, rt sdk.Runtime, a sdk.Agent, p sdk.Perception, dec *sdk.Decision) (string, error) {
	ma := fromSDKAgent(a)
	dec2 := toInternalDecision(dec)
	out := e.applyAction(ma, dec2)
	_ = db.RecordAction(rt.DB(), models.AgentAction{
		AgentID:    ma.ID,
		Action:     dec2.Action,
		TargetType: dec2.TargetKind,
		TargetID:   dec2.Target,
		Output:     out,
		Thought:    dec2.Reason,
	})
	// M5+M7：酒店动作对状态与需求的影响（酒店专属规则）
	delta := StateDelta{}
	switch dec2.Action {
	case "checkin":
		delta.Energy = -10
		delta.SocialNeed = -5
		delta.Mood = 3
		delta.NeedAchievement = -8 // 办成入住，满足成就
	case "checkout":
		delta.Energy = -5
		delta.Mood = 1
		delta.NeedAchievement = -4
	case "clean":
		delta.Energy = -8
		delta.Mood = 1
		delta.NeedAchievement = -5 // 完成任务满足成就
	case "maintain":
		delta.Energy = -8
		delta.Curiosity = 3
		delta.NeedKnowledge = -4
	case "review":
		delta.Mood = 2
		delta.Curiosity = 1
		delta.NeedKnowledge = -5 // 复盘满足求知
	}
	if delta.Energy != 0 || delta.Mood != 0 || delta.SocialNeed != 0 || delta.Curiosity != 0 ||
		delta.NeedAchievement != 0 || delta.NeedKnowledge != 0 {
		_ = e.rt.ApplyStateDelta(toSDKAgent(ma), toSDKStateDelta(delta))
	}
	return out, nil
}

func (e *HotelExecutor) applyAction(a models.Agent, dec *llm.Decision) string {
	rt := e.rt
	out := ""
	switch dec.Action {
	case "checkin":
		if dec.Target == 0 {
			break
		}
		room, err := db.RoomByID(rt.DB(), dec.Target)
		if err != nil || room.Status != db.RoomAvailable {
			out = "入住失败：房间不存在或已被占用"
			break
		}
		old, _ := db.SetRoomStatus(rt.DB(), room.ID, db.RoomOccupied)
		if old == db.RoomAvailable {
			b := &models.HotelBooking{
				AgentID: a.ID, AgentName: a.Name,
				RoomID: room.ID, RoomNumber: room.Number,
				CheckIn: time.Now(), CheckOut: time.Now().Add(24 * time.Hour),
				Status: "active",
			}
			_ = db.InsertBooking(rt.DB(), b)
			out = fmt.Sprintf("入住 %s 房成功", room.Number)
			rt.PublishEvent(bus.Event{Type: "hotel_checkin", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "CHECKIN", Detail: room.Number})
		}
	case "checkout":
		if dec.Target == 0 {
			break
		}
		// 找到该预订对应的房间并置为待清洁
		var booking models.HotelBooking
		_ = rt.DB().First(&booking, dec.Target)
		if booking.ID != 0 {
			_ = db.CheckoutBooking(rt.DB(), booking.ID)
			_, _ = db.SetRoomStatus(rt.DB(), booking.RoomID, db.RoomCleaning)
			out = fmt.Sprintf("退房 %s 房，已安排清洁", booking.RoomNumber)
			rt.PublishEvent(bus.Event{Type: "hotel_checkout", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "CHECKOUT", Detail: booking.RoomNumber})
		}
	case "clean":
		if dec.Target == 0 {
			break
		}
		old, _ := db.SetRoomStatus(rt.DB(), dec.Target, db.RoomAvailable)
		if old == db.RoomCleaning {
			r, _ := db.RoomByID(rt.DB(), dec.Target)
			out = fmt.Sprintf("清洁完成 %s 房，已可入住", r.Number)
			rt.PublishEvent(bus.Event{Type: "hotel_clean", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "CLEAN", Detail: r.Number})
		} else {
			out = "清洁跳过：房间非待清洁状态"
		}
	case "maintain":
		if dec.Target == 0 {
			break
		}
		old, _ := db.SetRoomStatus(rt.DB(), dec.Target, db.RoomMaintenance)
		if old == db.RoomAvailable {
			r, _ := db.RoomByID(rt.DB(), dec.Target)
			out = fmt.Sprintf("已安排 %s 房检修", r.Number)
			rt.PublishEvent(bus.Event{Type: "hotel_maintain", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "MAINTAIN", Detail: r.Number})
		} else {
			out = "检修跳过：房间非空房"
		}
	case "review":
		score := 3 + rand.Intn(3) // 3~5
		rv := &models.HotelReview{AgentID: a.ID, AgentName: a.Name, Score: score, Comment: "今日运营复盘"}
		_ = db.InsertReview(rt.DB(), rv)
		out = fmt.Sprintf("今日入住复盘：评分 %d", score)
		rt.PublishEvent(bus.Event{Type: "hotel_review", Time: now(), AgentID: a.ID, AgentName: a.Name, Avatar: a.Avatar, Action: "REVIEW", Detail: fmt.Sprintf("score=%d", score)})
	case "nothing":
		out = "无动作"
	default:
		out = "未知动作"
	}
	return out
}
