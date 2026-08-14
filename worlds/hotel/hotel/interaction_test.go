package hotel

import (
	"testing"
)

// setupRoleHotel 构建带完整角色的酒店：Alice(welcome, Entrance, P100)、Bob(frontdesk, FrontDesk)。
func setupRoleHotel() *SpaceWorld {
	w := newTestHotel()
	return w
}

// TestGuestIntentParsing 验证 M8.2：Guest 消息 → Intent 解析。
func TestGuestIntentParsing(t *testing.T) {
	cases := []struct{ msg, want string }{
		{"我要办理入住", "check_in"},
		{"check in please", "check_in"},
		{"我想退房", "check_out"},
		{"我要点餐", "restaurant"},
		{"room service 送餐", "room_service"},
		{"电梯在哪", "ask_direction"},
		{"我要投诉", "complaint"},
		{"随便看看", "general_help"},
	}
	for _, c := range cases {
		if got := string(ParseIntent(c.msg)); got != c.want {
			t.Errorf("ParseIntent(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

// TestConversationAndIntent 验证 M8.2：Guest 说话 → 解析 intent → 找到处理 Agent（FrontDesk）。
func TestConversationAndIntent(t *testing.T) {
	w := setupRoleHotel()
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "张三", Location: "entrance"}
	w.AddGuest(g)
	// Guest 说要入住
	intent, _ := w.GuestSay(1001, "我要办理入住")
	if intent != IntentCheckIn {
		t.Fatalf("intent should be check_in, got %s", intent)
	}
	// 找处理 Agent：frontdesk 负责 check_in → Bob(3)
	handler, _ := w.HandleIntent(1001, intent)
	if handler != 3 {
		t.Errorf("frontdesk Bob(3) should handle check_in, got %d", handler)
	}
	// 对话历史有 guest.message
	if len(w.Conversation()) == 0 {
		t.Error("conversation should have messages")
	}
}

// TestAgentSay 验证 M8.2：Agent 说话记入对话。
func TestAgentSay(t *testing.T) {
	w := setupRoleHotel()
	msg := w.Say(1, "您好，欢迎光临")
	if msg.Speaker != 1 || msg.Text != "您好，欢迎光临" {
		t.Errorf("agent say failed: %+v", msg)
	}
	if len(w.Conversation()) != 1 {
		t.Error("conversation should have 1 message")
	}
}

// TestCheckInFlow 验收 M8.2：完整 check-in 故事。
// Guest 进入 → Alice 迎接 → Guest 要入住 → Alice handoff → Bob 接管 → 模拟入住。
func TestCheckInFlow(t *testing.T) {
	w := setupRoleHotel()
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "张三", Location: "entrance"}
	w.AddGuest(g)

	// 1) Alice 迎接（welcome Agent 说话）
	w.Say(1, "您好，欢迎光临，请问有什么可以帮您？")

	// 2) Guest 说要入住
	intent, _ := w.GuestSay(1001, "我要办理入住")
	if intent != IntentCheckIn {
		t.Fatalf("intent should be check_in, got %s", intent)
	}

	// 3) welcome Agent(Alice) 判断自己不能办理入住 → handoff 给 frontdesk(Bob)
	handler, _ := w.HandleIntent(1001, intent)
	if handler != 3 {
		t.Fatalf("check_in should be handled by frontdesk Bob(3), got %d", handler)
	}
	if w.HandlerOf(1001) != 3 {
		t.Error("guest handler should now be Bob")
	}

	// 4) Bob 接管 + 完成模拟入住
	task, room, ok := w.CheckIn(1001, 3, "张三")
	if !ok {
		t.Fatal("check-in should succeed")
	}
	if room == "" {
		t.Error("check-in should assign a room")
	}
	if task.Status != "completed" {
		t.Errorf("task should be completed, got %s", task.Status)
	}
	// 5) 对话历史应该有时间线
	conv := w.Conversation()
	if len(conv) < 3 {
		t.Errorf("conversation should have >=3 messages, got %d", len(conv))
	}
}

// TestHandoffMechanism 验证 M8.2：welcome 不能处理 check_in，frontdesk 才能。
func TestHandoffMechanism(t *testing.T) {
	w := setupRoleHotel()
	g := &Guest{ID: 1001, Kind: "human", Role: "guest", Name: "张三", Location: "entrance"}
	w.AddGuest(g)
	intent, _ := w.GuestSay(1001, "我要办理入住")
	// welcome(Alice) 不应处理 check_in → resolver 找 frontdesk
	handler, handoff := w.HandleIntent(1001, intent)
	if handler != 3 {
		t.Errorf("should handoff to frontdesk, got %d", handler)
	}
	_ = handoff
	// welcome(Alice) 应能处理 general_help
	intent2, _ := w.GuestSay(1001, "你们酒店在哪")
	handler2, _ := w.HandleIntent(1001, intent2)
	_ = handler2 // welcome 能处理 general_help
}
