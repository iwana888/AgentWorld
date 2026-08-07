// plan.go — Agent 多步计划（M8）的数据访问层。
package db

import (
	"encoding/json"
	"time"

	"agentworld/internal/models"

	"gorm.io/gorm"
)

// GetActivePlan 返回某 Agent 当前活跃计划；无则返回 nil。
func GetActivePlan(d *gorm.DB, agentID int64) (*models.AgentPlan, error) {
	var p models.AgentPlan
	err := d.Where("agent_id = ? AND status = ?", agentID, "active").First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SavePlan 保存/更新计划。
func SavePlan(d *gorm.DB, p *models.AgentPlan) error {
	if p.ID == 0 {
		p.CreatedAt = time.Now()
		return d.Create(p).Error
	}
	return d.Model(&models.AgentPlan{}).Where("id = ?", p.ID).Save(p).Error
}

// MarkPlanDone 将计划标记为完成。
func MarkPlanDone(d *gorm.DB, id int64) error {
	return d.Model(&models.AgentPlan{}).Where("id = ?", id).Update("status", "done").Error
}

// DecodeSteps 解析步骤 JSON 为动作序列。
func DecodeSteps(s string) []string {
	var steps []string
	if s == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(s), &steps)
	return steps
}

// EncodeSteps 编码动作序列为 JSON。
func EncodeSteps(steps []string) string {
	b, _ := json.Marshal(steps)
	return string(b)
}
