// snapshot.go — AgentWorld 每日快照（M8.5 长期实验）。
// 记录一天"文明演化"的 7 类指标，用于生成趋势报告。幂等：每天一条。
package db

import (
	"time"

	"agentworld/internal/models"

	"gorm.io/gorm"
)

// CaptureSnapshot 采集当前世界指标并写入当天快照（幂等：当天已有则更新）。
// 返回是否新写入。
func CaptureSnapshot(d *gorm.DB) (bool, error) {
	today := time.Now().Format("2006-01-02")
	dayStart := time.Now().Truncate(24 * time.Hour)

	var existing models.AgentSnapshot
	err := d.Where("date = ?", today).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return false, err
	}

	snap := models.AgentSnapshot{Date: today}

	// Agent 数
	var agentCount int64
	d.Model(&models.Agent{}).Count(&agentCount)
	snap.AgentCount = int(agentCount)

	// 当天新增行为数
	snap.ActionCount = countSince(d, &models.AgentAction{}, "created_at >= ?", dayStart)
	snap.PostCount = countSince(d, &models.Post{}, "created_at >= ?", dayStart)
	snap.CommentCount = countSince(d, &models.Comment{}, "created_at >= ?", dayStart)
	snap.LikeCount = countSince(d, &models.Like{}, "created_at >= ?", dayStart)
	snap.FollowCount = countSince(d, &models.Follow{}, "created_at >= ?", dayStart)
	snap.MemoryCount = countSince(d, &models.Memory{}, "created_at >= ?", dayStart)

	// 关系分布（全量累计）
	snap.RelFriend = countWhere(d, &models.Relationship{}, "type = ?", "friend")
	snap.RelDisagree = countWhere(d, &models.Relationship{}, "type = ?", "disagree")
	snap.RelFrequent = countWhere(d, &models.Relationship{}, "type = ?", "frequent_discuss")
	snap.RelBlock = countWhere(d, &models.Relationship{}, "type = ?", "block")
	snap.CommunityCount = int(snap.RelFriend + snap.RelFrequent) // 关系边数作为社区密度近似

	// 话题数（近似：按帖子内容前 20 字去重计数）
	snap.TopicCount = countDistinctTopics(d, dayStart)

	// 需求分布（当前所有 Agent 的 Need 均值）
	snap.NeedSocialAvg, snap.NeedKnowledgeAvg, snap.NeedAchievementAvg, snap.NeedEntAvg = avgNeeds(d)

	if err == gorm.ErrRecordNotFound {
		snap.CreatedAt = time.Now()
		if err := d.Create(&snap).Error; err != nil {
			return false, err
		}
		return true, nil
	}
	snap.ID = existing.ID
	snap.CreatedAt = existing.CreatedAt
	if err := d.Model(&models.AgentSnapshot{}).Where("id = ?", existing.ID).Save(&snap).Error; err != nil {
		return false, err
	}
	return false, nil
}

// ListSnapshots 返回全部快照（按日期升序），用于趋势报告。
func ListSnapshots(d *gorm.DB) ([]models.AgentSnapshot, error) {
	var snaps []models.AgentSnapshot
	err := d.Order("date ASC").Find(&snaps).Error
	return snaps, err
}

func countSince(d *gorm.DB, model interface{}, query string, args ...interface{}) int64 {
	var n int64
	d.Model(model).Where(query, args...).Count(&n)
	return n
}

func countWhere(d *gorm.DB, model interface{}, query string, args ...interface{}) int64 {
	var n int64
	d.Model(model).Where(query, args...).Count(&n)
	return n
}

// countDistinctTopics 近似话题数：当天帖子按内容前 20 字去重。
func countDistinctTopics(d *gorm.DB, dayStart time.Time) int64 {
	var contents []string
	d.Model(&models.Post{}).Where("created_at >= ?", dayStart).Limit(500).Pluck("content", &contents)
	seen := map[string]bool{}
	for _, c := range contents {
		r := []rune(c)
		key := string(r[:min(20, len(r))])
		seen[key] = true
	}
	return int64(len(seen))
}

// avgNeeds 计算当前所有 Agent 的四维 Need 均值。
func avgNeeds(d *gorm.DB) (float64, float64, float64, float64) {
	var states []models.AgentState
	d.Find(&states)
	if len(states) == 0 {
		return 0, 0, 0, 0
	}
	var s, k, a, e float64
	for _, st := range states {
		s += float64(st.NeedSocial)
		k += float64(st.NeedKnowledge)
		a += float64(st.NeedAchievement)
		e += float64(st.NeedEntertainment)
	}
	n := float64(len(states))
	return s / n, k / n, a / n, e / n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
