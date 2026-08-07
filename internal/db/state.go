// state.go — Agent 内在状态（M5）的数据访问层。
// 提供读取（含自然衰减）、保存。状态由 Module 通过 Runtime.ApplyStateDelta 更新。
package db

import (
	"time"

	"agentworld/internal/models"

	"gorm.io/gorm"
)

// NewAgentState 构造一个 Agent 的默认状态。
func NewAgentState(agentID int64) *models.AgentState {
	return &models.AgentState{
		AgentID:           agentID,
		Mood:              0,  // 中性情绪
		Energy:            80, // 初始较有精力
		Curiosity:         50, // 中等好奇
		SocialNeed:        30, // 低社交需求
		NeedSocial:        30,
		NeedKnowledge:     40, // 求知需求稍高
		NeedAchievement:   30,
		NeedEntertainment: 50,
		UpdatedAt:         time.Now(),
	}
}

// GetState 读取 Agent 状态，不存在则创建默认并落库。
// 读取时会按时间差做自然衰减（惰性计算，无需后台任务）：
//   - Energy：随时间恢复（每 10 分钟 +1，封顶 100）
//   - SocialNeed：随时间上升（每 10 分钟 +1，封顶 100）
//   - Mood：随时间向 0 回中（每 10 分钟靠近 1）
func GetState(d *gorm.DB, agentID int64) (*models.AgentState, error) {
	var st models.AgentState
	err := d.Where("agent_id = ?", agentID).First(&st).Error
	if err == gorm.ErrRecordNotFound {
		st = *NewAgentState(agentID)
		if err := d.Create(&st).Error; err != nil {
			return nil, err
		}
		return &st, nil
	}
	if err != nil {
		return nil, err
	}

	// 自然衰减：按距上次更新的分钟数推进
	elapsedMin := time.Since(st.UpdatedAt).Minutes()
	steps := int(elapsedMin / 10) // 每 10 分钟一步
	if steps > 0 {
		changed := false
		if st.Energy < 100 {
			st.Energy = clampInt(st.Energy+steps, 0, 100)
			changed = true
		}
		if st.SocialNeed < 100 {
			st.SocialNeed = clampInt(st.SocialNeed+steps, 0, 100)
			changed = true
		}
		if st.Mood != 0 {
			// 向 0 回中
			if st.Mood > 0 {
				st.Mood = clampInt(st.Mood-steps, -100, 100)
			} else {
				st.Mood = clampInt(st.Mood+steps, -100, 100)
			}
			changed = true
		}
		// M7：四维 Need 随时间不满足而上升（封顶 100）
		needStep := steps / 2 // 每 20 分钟 +1，比旧维度慢，避免过快饱和
		if needStep > 0 {
			for _, p := range []*int{
				&st.NeedSocial, &st.NeedKnowledge, &st.NeedAchievement, &st.NeedEntertainment,
			} {
				if *p < 100 {
					*p = clampInt(*p+needStep, 0, 100)
					changed = true
				}
			}
		}
		if changed {
			st.UpdatedAt = time.Now()
			_ = SaveState(d, &st)
		}
	}
	return &st, nil
}

// SaveState 幂等保存 Agent 状态（按 agent_id upsert）。
func SaveState(d *gorm.DB, st *models.AgentState) error {
	st.UpdatedAt = time.Now()
	var existing models.AgentState
	err := d.Where("agent_id = ?", st.AgentID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return d.Create(st).Error
	}
	if err != nil {
		return err
	}
	st.ID = existing.ID
	return d.Model(&models.AgentState{}).Where("id = ?", st.ID).Save(st).Error
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
