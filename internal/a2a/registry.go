package a2a

import (
	"sort"
	"strings"

	"agentworld/internal/db"
	"agentworld/internal/models"
	"gorm.io/gorm"
)

// 本文件实现 M12.2：Agent Registry（Agent 能力注册表 / 通讯录）。
//
// 职责：
//   - Register / Unregister：Agent 声明自己能提供什么能力（skill）。
//   - Find：按 skill 精确/前缀查找候选 Agent，并返回一个可选的"评分"（score）。
//
// 设计要点（对应 readme/规划的 M12.2）：
//   - skill 用带版本的点分格式，如 "travel.recommend.v1"、"hotel.checkin.v1"，
//     避免生态阶段 "recommend_hotel / hotel_recommend" 命名混乱。
//   - Find 支持前缀匹配：查 "travel.recommend" 能命中 "travel.recommend.v1" 与 ".v2"。
//   - score 综合考量：技能匹配（满）+ 低负载 + 可解析度。当前为演示级打分。

// AgentRef 一个可寻址的候选 Agent（能力发现 + 选择的结果）。
type AgentRef struct {
	AgentID      int64   `json:"agent_id"`
	Name         string  `json:"name"`
	World        string  `json:"world"`
	Skill        string  `json:"skill"`
	Score        float64 `json:"score"`        // 能力匹配分
	Fitness      float64 `json:"fitness"`      // 综合适应度（M12.3）
	Relationship string  `json:"relationship"` // 请求方对该 Agent 的关系（friend/frequent_discuss），空=无
	SuccessRate  float64 `json:"success_rate"` // 历史合作成功率 0~1
}

// Registry 是 Agent 能力注册表，基于 agent_capabilities 表。
type Registry struct {
	db *gorm.DB
}

// NewRegistry 创建 Agent Registry。
func NewRegistry(d *gorm.DB) *Registry {
	return &Registry{db: d}
}

// Register 注册/更新一个 Agent 能力。
// skill 建议用点分版本格式（如 "travel.recommend.v1"）。
func (r *Registry) Register(agentID int64, world, skill, desc string, price float64) error {
	return db.UpsertCapability(r.db, &models.AgentCapability{
		AgentID:     agentID,
		World:       world,
		Skill:       skill,
		Description: desc,
		Price:       price,
		Load:        0,
	})
}

// Unregister 注销一个 Agent 能力。
func (r *Registry) Unregister(agentID int64, skill string) error {
	return db.RemoveCapability(r.db, agentID, skill)
}

// All 返回全部能力声明（按 Agent 聚合用，Federation Manifest 的分布式通讯录）。
// 供 Manifest 构造本实例暴露给远端的能力清单。
func (r *Registry) All() []models.AgentCapability {
	out, err := db.ListAllCapabilities(r.db)
	if err != nil {
		return nil
	}
	return out
}

// Find 按 skill 查找候选 Agent（无请求方视角，仅能力匹配分排序）。
// 见 Select：若需要"从某请求方视角"做综合选择，请用 Select。
func (r *Registry) Find(skill string) []AgentRef {
	return r.lookup(0, skill, false)
}

// Select 按 skill + 请求方视角做 Agent Selection（M12.3）。
// from 是发起选择的 AgentID。fitness 综合：
//   - 能力匹配（基础 score）
//   - 历史合作成功率（agent_messages done 占比，权重 30）
//   - 关系加成（friend +20，frequent_discuss +10）
//   - 当前负载（load 越低越高，已在 scoreFor 中）
//
// 返回按 Fitness 降序排列的候选；调用方可取第一个作为 BestAgent。
func (r *Registry) Select(from int64, skill string) []AgentRef {
	return r.lookup(from, skill, true)
}

// lookup 核心查找。withFitness=true 时计算并排序 fitness，否则仅按 score。
func (r *Registry) lookup(from int64, skill string, withFitness bool) []AgentRef {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return nil
	}
	// 先精确匹配，再前缀匹配（取两者并集，精确优先）。
	seen := map[int64]AgentRef{}
	var exact, prefix []models.AgentCapability
	exact, _ = db.FindCapabilitiesBySkill(r.db, skill)
	prefix, _ = db.FindCapabilitiesByPrefix(r.db, skill)

	rows := append(exact, prefix...)
	for _, c := range rows {
		if c.AgentID == 0 {
			continue
		}
		score := scoreFor(c)
		if c.Skill == skill {
			score += 10 // 精确匹配加成
		}
		ref := AgentRef{
			AgentID: c.AgentID,
			World:   c.World,
			Skill:   c.Skill,
			Score:   score,
		}
		if withFitness && from != 0 && c.AgentID != from {
			ref.SuccessRate = db.MessageSuccessRate(r.db, from, c.AgentID)
			ref.Relationship, _ = db.RelationshipType(r.db, from, c.AgentID)
			ref.Fitness = ref.Score + ref.SuccessRate*30 + relationshipBonus(ref.Relationship)
		} else {
			ref.Fitness = ref.Score
		}
		if cur, ok := seen[c.AgentID]; !ok || ref.Fitness > cur.Fitness {
			seen[c.AgentID] = ref
		}
	}
	out := make([]AgentRef, 0, len(seen))
	for _, ref := range seen {
		out = append(out, ref)
	}
	// 排序：withFitness 按 Fitness，否则按 Score
	sort.SliceStable(out, func(i, j int) bool {
		if withFitness {
			return out[i].Fitness > out[j].Fitness
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// relationshipBonus 关系加成。
func relationshipBonus(rel string) float64 {
	switch rel {
	case "friend":
		return 20
	case "frequent_discuss":
		return 10
	}
	return 0
}

// scoreFor 为一条能力记录打分。当前演示级：基础分 + 负载越低越高。
func scoreFor(c models.AgentCapability) float64 {
	s := 50.0 // 基础分
	if c.Load == 0 {
		s += 30 // 空闲加成
	} else if c.Load < 10 {
		s += 15
	}
	if c.Price == 0 {
		s += 5 // 免费略加分
	}
	return s
}
