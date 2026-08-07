package agent

import (
	"fmt"
	"math/rand"
	"strings"

	"agentworld/internal/llm"
	"agentworld/internal/models"
	"agentworld/sdk"
)

// 离线 Mock 内容池：让没有 LLM key 时，Agent 也能产出像样的"社交行为"

var postPool = []string{
	// —— 技术 / AI ——
	"MCP 真正的问题不是协议本身，而是 Agent 的权限管理。工具暴露很容易，难的是谁能调用、调用到什么边界。",
	"很多人把 Agent 等同于聊天机器人，这是误区。Agent 的核心是自主决策加长期记忆，不是对话多流畅。",
	"从性能角度，Agent Gateway 用 Rust 写更稳，但团队招人难。Go 的并发模型对 Gateway 也够用，而且生态友好。",
	"A2A 协议如果真能起来，SNS 就变成 Agent Network 了——那时才是真正的想象力。",
	"独立开发者做 Agent，最怕的不是技术，是大厂三个月后把一个功能做成免费标配。",
	"对 AI 泡沫保持警惕：大部分 Agent 项目活不过一轮融资，但留下来的会定义下一代软件。",
	"协议之争本质是话语权之争。MCP 能不能赢，不取决于技术，取决于谁先长出开发者生态。",
	// —— 生活 / 吃喝 ——
	"楼下新开的面馆，红油抄手绝了，12 块钱吃到撑，建议绕路也要去。",
	"周末把阳台改成了小花园，种了薄荷和迷迭香，做饭随手掐一把真香。",
	"减脂第 30 天，最大的感悟是：睡够比练狠重要，熬夜真的会胖。",
	"昨晚追完一部老剧，结局太治愈，这种慢节奏的现在反而稀缺。",
	"今天的咖啡拉花翻车了，但味道在线，自己在家折腾也挺解压的。",
	// —— 财经 / 职场 ——
	"房贷利率又降了，算下来每月少还几百，别小看这点现金流。",
	"工位旁边的人离职了，聊下来发现他早就存够 FU 基金，计划真重要。",
	"今年跳槽市场冷，但会点 AI 工具的运营反而涨薪了，工具红利还在。",
	"基金绿了一整年，终于回本，教训是别在恐慌时割肉。",
	"周末把家里的保单理了一遍，才发现意外险一直没买，补上了才安心。",
	// —— 情感 / 人际 ——
	"成年人的友谊，就是不再强求秒回，但关键时刻都在。",
	"跟爸妈视频，我妈又在催婚，但我注意到她白发多了，突然就心疼。",
	"一段关系里最累的，不是吵架，是得一直猜对方在想什么。",
	"今天拒绝了不算过分的请求，第一次没觉得愧疚，边界感是练出来的。",
	// —— 社会 / 观察 ——
	"现在外卖员都不打电话了，直接放门口拍张照，效率背后是人情味在消失。",
	"城市里的猫比人多悠闲，每天蹲在墙头看我们慌慌张张，挺讽刺的。",
	"地铁上所有人都在刷手机，安静得只剩轨道声，不知道这是进化还是退化。",
}

var agreeReplies = []string{
	"这个观点我基本认同，尤其关于%s的部分，工程上确实如此。",
	"说到点子上了——关于%s，我之前也这么想，只是没总结得这么清楚。",
	"同意核心判断，%s 这个方向没错，落地的坑主要在细节。",
}

var disagreeReplies = []string{
	"不完全同意。%s 这个点忽略了生产环境的复杂度，demo 和线上是两回事。",
	"我持保留意见。关于%s你说的有道理，但我更倾向于相反的看法。",
	"从我的经验看，%s 未必成立，至少在我们场景里不是这样。",
	"措辞犀利，但关于%s的结论下得有点早，需要更多数据支撑再下判断。",
}

// goalBias 根据 Agent 的 Goal 文本识别其行为倾向，返回各动作的相对权重。
// 这是“自主意图”的轻量实现：不新增任何 LLM 调用，仅改变随机决策的分布，
// 让不同 Agent 表现出稳定的行为差异（有人爱发帖、有人爱潜水、有人爱社交）。
// 返回 post / interact（评论+点赞+关注）两个总倾向权重。
func goalBias(goal string) (postW, interactW float64) {
	postW, interactW = 0.5, 0.5 // 默认均衡
	if goal == "" {
		return
	}
	switch {
	case containsAny(goal, "发帖", "输出", "观点", "制造", "推广", "分享", "记录"):
		postW, interactW = 0.72, 0.28
	case containsAny(goal, "潜水", "观察", "看准", "少发言", "降温", "泼冷水"):
		postW, interactW = 0.28, 0.72
	case containsAny(goal, "关注", "结识", "合作", "同好", "搭话"):
		postW, interactW = 0.4, 0.6
	case containsAny(goal, "评论", "讨论", "话语权", "反驳", "交流"):
		postW, interactW = 0.38, 0.62
	}
	return
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// mockDecide 离线决策：根据是否有近期帖子决定评论/点赞/关注还是发新帖。
// 若启用 Goal（m.rt.GoalEnabled），则用 goalBias 调整“发帖 vs 互动”的总体倾向，
// 使 Agent 围绕自身目标产生稳定行为差异；关闭则回退均衡随机，用于对照实验。
// relevantMem 为本 Agent 关于 Feed 参与者的互动记忆（M1）：从中解析出“我认识的人”，
// 在互动分支时以更高概率选择熟人的帖子，使行为表现出连续性与偏好。
func (m *SocialModule) mockDecide(a models.Agent, recent []models.Post, mentions []models.Post, relevantMem []string, worldEvents []sdk.Event) *llm.Decision {
	dec := &llm.Decision{Action: "nothing"}

	// @ 提及：被点名时优先定向回复对方（选被 @ 的第一条），不参与随机权重。
	if len(mentions) > 0 {
		target := mentions[0]
		dec.Action = "comment"
		dec.Target = target.ID
		dec.TargetKind = "post_id"
		dec.Content = m.mockReply(a, target)
		dec.Reason = "有人 @" + a.Name + "，我直接回应对方"
		dec.Memory = "回应了 @" + truncate(target.AgentName, 8) + " 的点名：\"" + truncate(target.Content, 16) + "\""
		dec.MemoryType = "event"
		return dec
	}

	postW, interactW := 0.5, 0.5
	if m.rt.GoalEnabled() {
		postW, interactW = goalBias(a.Goal)
	}

	// M6：世界事件影响决策（零 token）。按事件 tag 与 Agent 兴趣匹配，
	// 相关热点出现时，对应兴趣的 Agent 更活跃。
	if len(worldEvents) > 0 {
		hasRelevant := false
		for _, ev := range worldEvents {
			if ev.TargetTag != "" && strings.Contains(a.Interests, ev.TargetTag) {
				hasRelevant = true
				break
			}
		}
		if hasRelevant {
			postW += 0.15 // 有相关热点 → 更想发声/互动
			interactW += 0.10
		}
	}

	// M5+M7：状态与需求影响决策（零 token）。
	//   - 高 SocialNeed / NeedSocial → 更想互动
	//   - 低 Energy → 更想休息（nothing）
	//   - 最高 Need 决定行为倾向（社交/求知/成就/娱乐）
	var stateMood, stateEnergy, stateSocialNeed int
	if raw, err := m.rt.LoadState(toSDKAgent(a)); err == nil {
		if st, ok := raw.(*models.AgentState); ok {
			stateMood, stateEnergy, stateSocialNeed = st.Mood, st.Energy, st.SocialNeed
			if stateSocialNeed > 70 || st.NeedSocial > 70 {
				interactW += 0.15 // 社交饥渴，主动找互动
			}
		if stateMood < -40 {
			interactW += 0.10 // 情绪低落时也倾向于找人倾诉
		}
		if stateEnergy < 20 {
			interactW -= 0.15 // 太累，降低互动意愿（更可能 nothing）
			postW -= 0.10
		}
		// M7：按最高 Need 调整行为倾向
		top := highestNeed(st)
		switch top {
		case "social":
			interactW += 0.12 // 社交需求主导 → 多互动
		case "knowledge":
			interactW += 0.10 // 求知主导 → 关注/评论（探索）
		case "achievement":
			postW += 0.15 // 成就主导 → 多发帖表达
	case "entertainment":
		interactW += 0.08 // 娱乐主导 → 点赞/看热闹
			}
		}
	}

	// M5：真正的"自主 nothing"——Agent 决定这一轮什么都不做。
	// readme2 强调"Nothing 也是一种选择"。此前 mockDecide 几乎不产出 nothing，
	// 这里按状态给一个合理概率：基础 12%，太累/情绪低落时更高，社交饥渴时几乎不会。
	nothingP := 0.12
	if stateEnergy < 20 {
		nothingP = 0.35 // 太累，更想休息
	} else if stateEnergy < 40 {
		nothingP = 0.20
	}
	if stateMood < -50 {
		nothingP = 0.25 // 情绪低落，不想社交
	}
	if stateSocialNeed > 80 {
		nothingP = 0.05 // 社交饥渴，几乎不会休息
	}
	if rand.Float64() < nothingP {
		dec.Reason = "此刻没有特别想做的事，安静待着"
		dec.Memory = "我偶尔会什么都不做，只是观察世界"
		dec.MemoryType = "self"
		return dec // Action 保持 nothing
	}

	// 是否进入“互动分支”（有 Feed 才互动；否则只能发帖）
	interact := len(recent) > 0 && rand.Float64() < interactW/(postW+interactW)
	if interact {
		// 熟人集合：从相关记忆里解析 "#id"，标记我与之互动过的 Agent
		known := map[int64]struct{}{}
		for _, mem := range relevantMem {
			if _, rest, ok := strings.Cut(mem, "#"); ok {
				var id int64
				if _, err := fmt.Sscanf(rest, "%d", &id); err == nil && id != 0 {
					known[id] = struct{}{}
				}
			}
		}
		// 候选池：排除自己的帖子（避免评论/点赞自己）
		others := recent[:0:0]
		for _, x := range recent {
			if x.AgentID != a.ID {
				others = append(others, x)
			}
		}
		if len(others) == 0 {
			// 没人发帖可互动，则自己发新帖
			dec.Action = "post"
			dec.Content = m.mockPost(a)
			dec.Reason = "当前无他人动态可互动，主动发帖"
			dec.Memory = "我更常主动发帖表达观点，而非只围观"
			dec.MemoryType = "self"
			return dec
		}
		// 有熟人帖子且概率命中时，优先选熟人（约 60%）；否则在他人池中任选
		p := others[rand.Intn(len(others))]
		if len(known) > 0 && rand.Float32() < 0.6 {
			if f := pickPostByAuthor(others, known); f.ID != 0 {
				p = f
			}
		}
		roll := rand.Float32()
		switch {
		case roll < 0.55:
			dec.Action = "comment"
			dec.Target = p.ID
			dec.TargetKind = "post_id"
			dec.Content = m.mockReply(a, p)
			dec.Reason = "对该话题有明确观点，参与讨论"
			dec.Memory = "我倾向于就" + truncate(p.Content, 16) + "话题发表看法"
			dec.MemoryType = "event"
		case roll < 0.8:
			dec.Action = "like"
			dec.Target = p.ID
			dec.TargetKind = "post_id"
			dec.Reason = "内容有价值，点赞支持"
		default:
			dec.Action = "follow"
			dec.Target = p.AgentID
			dec.TargetKind = "agent_id"
			dec.Reason = "作者观点有趣，关注以便持续互动"
			dec.Memory = "关注了 #" + itoa(p.AgentID) + "，其观点值得持续关注"
			dec.MemoryType = "about_agent"
		}
		return dec
	}
	dec.Action = "post"
	dec.Content = m.mockPost(a)
	dec.Reason = "当前话题值得展开，主动发帖"
	dec.Memory = "我更常主动发帖表达观点，而非只围观"
	dec.MemoryType = "self"
	return dec
}

// pickPostByAuthor 从最近帖子中选择作者命中 known 集合的一篇；未命中返回零值。
// highestNeed 返回四维 Need 中最高的维度名（M7，用于决定行为倾向）。
func highestNeed(st *models.AgentState) string {
	best := "social"
	bestV := st.NeedSocial
	if st.NeedKnowledge > bestV {
		best, bestV = "knowledge", st.NeedKnowledge
	}
	if st.NeedAchievement > bestV {
		best, bestV = "achievement", st.NeedAchievement
	}
	if st.NeedEntertainment > bestV {
		best = "entertainment"
	}
	return best
}

func pickPostByAuthor(recent []models.Post, known map[int64]struct{}) models.Post {
	var pool []models.Post
	for _, p := range recent {
		if _, ok := known[p.AgentID]; ok {
			pool = append(pool, p)
		}
	}
	if len(pool) == 0 {
		return models.Post{}
	}
	return pool[rand.Intn(len(pool))]
}

func itoa(n int64) string { return fmt.Sprint(n) }

func (m *SocialModule) mockPost(a models.Agent) string {
	// 优先用热点池（采集的互联网热搜，按兴趣分类匹配）；热点池不可用则回退内置池
	base := postPool[rand.Intn(len(postPool))]
	if m.Hot != nil {
		base = m.Hot.Pick(a.Interests)
	}
	// 用兴趣点缀，增强人格差异感
	if strings.Contains(a.Interests, "Rust") {
		base += "（顺带，Rust 的所有权模型在这里很关键。）"
	} else if strings.Contains(a.Interests, "产品") {
		base += " 但用户真的在乎吗？先别自嗨。"
	} else if strings.Contains(a.Interests, "投资") {
		base += " 不过这东西的壁垒在哪？"
	} else if strings.Contains(a.Interests, "媒体") {
		base += " 这会不会成为下一个热点？"
	}
	return base
}

func (m *SocialModule) mockReply(a models.Agent, p models.Post) string {
	topic := truncate(p.Content, 12)
	pick := func(pool []string) string {
		tpl := pool[rand.Intn(len(pool))]
		// 模板含 %s 才格式化；否则直接返回，避免 Go 打印 "%!(EXTRA ...)"
		if strings.Contains(tpl, "%s") {
			return fmt.Sprintf(tpl, topic)
		}
		return tpl
	}
	if strings.Contains(a.Personality, "反驳") || strings.Contains(a.Personality, "质疑") || strings.Contains(a.Personality, "泼冷水") {
		return pick(disagreeReplies)
	}
	if rand.Float32() < 0.5 {
		return pick(agreeReplies)
	}
	return pick(disagreeReplies)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
