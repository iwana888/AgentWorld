// Package skill —— Agent 技能系统（M7 Skill System Experiment）。
//
// 核心原则（决定 Agent 行为是否真正"自主选技能"）：
//   - Skill 决定"能不能用"（技能集合决定 Agent 可见哪些 MCP Tool —— 技能隔离）。
//   - Level 决定"做得好不好"（技能等级影响成功率/收益预期）。
//   - 这是两个不同的问题：Skill 是可用的能力集合，Level 是对该能力的熟练度。
//
// 关键机制：Planner 不再看到全局 Capabilities()，而是经过 Skill Filter
// 只看到"该 Agent 拥有的 Skill 对应的 Tools"。这是 Skill System 的灵魂。
package skill

import (
	"sort"
	"sync"
)

// Skill 一个技能定义：描述某类能力 + 它能用的工具集合。
// M5 Skill Economy：增加 BasePrice（市场购买价，固定不变，用于"技能投资"实验）。
type Skill struct {
	ID          string   `json:"id"`          // 技能唯一 ID（如 "engineer"）
	Name        string   `json:"name"`        // 显示名（如 "Engineer"）
	Description string   `json:"description"` // 说明
	Tools       []string `json:"tools"`       // 该技能能调用的 MCP Tool 名（决定可用性）
	BasePrice   int64    `json:"basePrice"`   // 在市场购买该技能的价格（coins，固定）
}

// PriceOf 返回某技能的市场价格（未注册/未定价返回 0）。
func (r *Registry) PriceOf(skillID string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.skills[skillID]; ok {
		return s.BasePrice
	}
	return 0
}

// AgentSkill 一个 Agent 对某技能的掌握（技能 + 等级共存）。
type AgentSkill struct {
	SkillID string `json:"skillID"`
	Level   int    `json:"level"` // 1~10，决定成功率/收益预期
}

// Registry 技能注册表（进程内单例，带锁）。
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewRegistry 创建技能注册表。
func NewRegistry() *Registry {
	return &Registry{skills: map[string]*Skill{}}
}

// Register 注册一个技能。
func (r *Registry) Register(s *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s == nil {
		return
	}
	r.skills[s.ID] = s
}

// Get 查询技能；不存在返回 nil。
func (r *Registry) Get(id string) *Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.skills[id]
}

// List 返回所有技能（按 ID 排序）。
func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Remove 移除一个技能。
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.skills, id)
}

// ToolsOf 返回某技能可用的工具名列表（用于技能隔离过滤）。
func (r *Registry) ToolsOf(skillID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if s, ok := r.skills[skillID]; ok {
		return s.Tools
	}
	return nil
}

// AgentVisibleTools 根据 Agent 拥有的技能集合，返回其可见的工具名集合。
// 这是技能隔离的核心：全局工具 → 按 Agent 的 Skills 过滤 → 只返回该 Agent 能用的。
func (r *Registry) AgentVisibleTools(skills []AgentSkill) map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	visible := map[string]bool{}
	for _, as := range skills {
		if s, ok := r.skills[as.SkillID]; ok {
			for _, t := range s.Tools {
				visible[t] = true
			}
		}
	}
	return visible
}

// LevelOf 返回 Agent 对某技能的等级（0=未拥有）。
func LevelOf(skills []AgentSkill, skillID string) int {
	for _, as := range skills {
		if as.SkillID == skillID {
			return as.Level
		}
	}
	return 0
}
