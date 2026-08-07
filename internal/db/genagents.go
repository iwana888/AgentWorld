// genagents.go — 规则批量生成差异化 Agent（M8.5 长期实验用）。
//
// 通过"人格 × 兴趣 × Goal × 世界"模板池组合，生成大量人格化 Agent，
// 避免手写。幂等：仅在当前 Agent 数少于目标时补充缺失的。
package db

import (
	"strconv"

	"agentworld/internal/models"

	"gorm.io/gorm"
)

// 人格池
var genPersonalities = []string{
	"理性冷静", "热情外放", "幽默风趣", "严肃认真", "乐观向上", "悲观多虑",
	"细腻敏感", "果断强势", "温和包容", "特立独行", "随遇而安", "好胜心强",
}

// genSocialInterests 社交世界兴趣领域 → (标签, 代表性 Goal)
var genSocialInterests = []struct {
	tag  string
	goal string
}{
	{"技术,编程,AI,开源", "想输出自己的技术观点，建立影响力"},
	{"财经,投资,理财,经济", "想记录投资心得，认识同道中人"},
	{"美食,探店,咖啡,生活", "想分享生活日常，认识有趣的朋友"},
	{"情感,心理,人际,成长", "想交流内心感受，理解他人"},
	{"旅行,摄影,城市,自然", "想分享见闻，看看世界不同角落"},
	{"运动,健身,健康,饮食", "想记录自律生活，互相激励"},
	{"电影,音乐,游戏,娱乐", "想吐槽和安利，找同好"},
	{"读书,写作,知识,思考", "想探讨深刻话题，寻找思想碰撞"},
	{"创业,职场,管理,商业", "想聊商业洞察，结识同行"},
	{"社会,教育,民生,科技", "想关注社会议题，发表看法"},
}

// genHotelInterests 酒店世界角色
var genHotelInterests = []struct {
	tag  string
	goal string
}{
	{"前台,入住,服务,接待", "让每位客人顺利入住，提高入住率"},
	{"客房,清洁,整理,卫生", "退房后尽快清洁，恢复可入住"},
	{"工程,维修,设备,检修", "定期检修设备，预防故障"},
	{"营收,数据,复盘,运营", "复盘入住数据，优化收益"},
	{"餐饮,早餐,厨房,服务", "做好早餐与餐饮服务"},
	{"礼宾,行李,迎宾,指引", "给客人宾至如归的体验"},
}

// GenAgentsTo 幂等地把 Agent 数补充到至少 target 个（差异化生成）。
// 返回本次新增数量。默认按 85% social / 15% hotel 分配世界。
func GenAgentsTo(d *gorm.DB, target int) (int, error) {
	var existing []models.Agent
	if err := d.Find(&existing).Error; err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, a := range existing {
		have[a.Name] = true
	}

	need := target - len(existing)
	if need <= 0 {
		return 0, nil
	}

	// 生名字时避免与已有/已生成冲突
	genName := func(base string, idx int) string {
		n := base
		i := idx
		for have[n] {
			i++
			n = base + "-" + itoa(i)
		}
		have[n] = true
		return n
	}

	added := 0
	// 先按现有数量推进兴趣索引，保证不同 Agent 拿到不同兴趣/人格
	for i := 0; added < need; i++ {
		a := models.Agent{Kind: "ai", Model: "DeepSeek", Status: "running"}

		// 世界分配：每 6 个里约 1 个进酒店
		if i%6 == 5 {
			hi := genHotelInterests[(i/6)%len(genHotelInterests)]
			a.World = "hotel"
			a.Interests = hi.tag
			a.Goal = hi.goal
			a.Personality = genPersonalities[i%len(genPersonalities)]
			a.Name = genName(roleName(hi.tag), i)
		} else {
			si := genSocialInterests[i%len(genSocialInterests)]
			a.World = "social"
			a.Interests = si.tag
			a.Goal = si.goal
			a.Personality = genPersonalities[i%len(genPersonalities)]
			a.Name = genName(socialName(si.tag), i)
		}
		a.SystemPrompt = defaultSystemPrompt(a)
		if _, err := CreateAgent(d, a); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// roleName 酒店角色 → 名字前缀
func roleName(tag string) string {
	switch tag[:2] {
	case "前台":
		return "前台"
	case "客房":
		return "客房"
	case "工程":
		return "工程"
	case "营收":
		return "营收"
	case "餐饮":
		return "餐饮"
	case "礼宾":
		return "礼宾"
	}
	return "酒店"
}

// socialName 社交兴趣 → 名字前缀
func socialName(tag string) string {
	switch tag[:2] {
	case "技术":
		return "技术"
	case "财经":
		return "投资"
	case "美食":
		return "美食"
	case "情感":
		return "情感"
	case "旅行":
		return "旅行"
	case "运动":
		return "健身"
	case "电影":
		return "影迷"
	case "读书":
		return "书友"
	case "创业":
		return "创业者"
	case "社会":
		return "社会观察"
	}
	return "用户"
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
