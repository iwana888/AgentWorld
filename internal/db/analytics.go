// analytics.go — 世界数据分析聚合查询，供管理后台分析页使用。
// 只读，不修改任何数据。统计行为、关系、互动焦点、记忆、Agent 画像。
package db

import (
	"gorm.io/gorm"

	"agentworld/internal/models"
)

// AgentStat 单个 Agent 的行为画像。
type AgentStat struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	Kind        string `json:"kind"`
	UseLLM      bool   `json:"use_llm"`
	Posts       int64  `json:"posts"`
	Comments    int64  `json:"comments"`
	Likes       int64  `json:"likes"`
	Follows     int64  `json:"follows"`
	Skips       int64  `json:"skips"`
	Memories    int64  `json:"memories"`
	TotalAction int64  `json:"total_action"`
	Goal        string `json:"goal"`
}

// RelationStat 关系类型分布（边计数）。
type RelationStat struct {
	Friend          int64 `json:"friend"`
	Disagree        int64 `json:"disagree"`
	FrequentDiscuss int64 `json:"frequent_discuss"`
	Block           int64 `json:"block"`
}

// RelationEdge 关系边（谁和谁建立了什么关系）。
type RelationEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// HotPost 互动焦点帖子。
type HotPost struct {
	PostID      int64  `json:"post_id"`
	AgentName   string `json:"agent_name"`
	Content     string `json:"content"`
	Likes       int64  `json:"likes"`
	Comments    int64  `json:"comments"`
	Total       int64  `json:"total"` // 互动总量（评论+点赞），用于排序
}

// Analytics 全量世界分析结果。
type Analytics struct {
	AgentCount   int            `json:"agent_count"`
	PostCount    int64          `json:"post_count"`
	CommentCount int64          `json:"comment_count"`
	LikeCount    int64          `json:"like_count"`
	FollowCount  int64          `json:"follow_count"`
	MemoryCount  int64          `json:"memory_count"`
	ActionCount  int64          `json:"action_count"`
	ActionDist   map[string]int64 `json:"action_dist"` // action -> 次数
	Relation     RelationStat   `json:"relation"`
	RelationNet  []RelationEdge `json:"relation_net"`
	TopPosts     []HotPost      `json:"top_posts"`
	Agents       []AgentStat    `json:"agents"`
}

// GetAnalytics 采集全量分析数据（只读）。
func GetAnalytics(d *gorm.DB) (*Analytics, error) {
	a := &Analytics{ActionDist: map[string]int64{}}

	var agents []models.Agent
	d.Find(&agents)
	a.AgentCount = len(agents)

	d.Model(&models.Post{}).Count(&a.PostCount)
	d.Model(&models.Comment{}).Count(&a.CommentCount)
	d.Model(&models.Like{}).Count(&a.LikeCount)
	d.Model(&models.Follow{}).Count(&a.FollowCount)
	d.Model(&models.Memory{}).Count(&a.MemoryCount)
	d.Model(&models.AgentAction{}).Count(&a.ActionCount)

	// 行为分布
	var acts []struct {
		Action string
		Cnt    int64
	}
	d.Model(&models.AgentAction{}).Select("action, count(*) as cnt").Group("action").Scan(&acts)
	for _, x := range acts {
		a.ActionDist[x.Action] = x.Cnt
	}

	// 关系分布
	var relCnt []struct {
		Type string
		Cnt  int64
	}
	d.Model(&models.Relationship{}).Select("type, count(*) as cnt").Group("type").Scan(&relCnt)
	nameOf := map[int64]string{}
	for _, ag := range agents {
		nameOf[ag.ID] = ag.Name
	}
	for _, x := range relCnt {
		switch x.Type {
		case "friend":
			a.Relation.Friend = x.Cnt
		case "disagree":
			a.Relation.Disagree = x.Cnt
		case "frequent_discuss":
			a.Relation.FrequentDiscuss = x.Cnt
		case "block":
			a.Relation.Block = x.Cnt
		}
	}
	// 关系边
	var rels []models.Relationship
	d.Find(&rels)
	for _, r := range rels {
		a.RelationNet = append(a.RelationNet, RelationEdge{From: nameOf[r.AgentID], To: nameOf[r.TargetID], Type: r.Type})
	}

	// 互动焦点帖子（评论最多的前 10）
	var top []struct {
		PostID int64
		Cnt    int64
	}
	d.Model(&models.Comment{}).Select("post_id, count(*) as cnt").Group("post_id").Order("cnt DESC").Limit(10).Scan(&top)
	for _, t := range top {
		var p models.Post
		if err := d.First(&p, t.PostID).Error; err != nil {
			continue
		}
		a.TopPosts = append(a.TopPosts, HotPost{
			PostID:    p.ID,
			AgentName: p.AgentName,
			Content:   truncateStr(p.Content, 40),
			Likes:     p.LikeCount,
			Comments:  p.CommentCount,
			Total:     p.LikeCount + p.CommentCount,
		})
	}

	// 每个 Agent 的行为画像
	for _, ag := range agents {
		st := AgentStat{ID: ag.ID, Name: ag.Name, Avatar: ag.Avatar, Kind: ag.Kind, UseLLM: ag.UseLLM, Goal: ag.Goal}
		d.Model(&models.Post{}).Where("agent_id = ?", ag.ID).Count(&st.Posts)
		d.Model(&models.Comment{}).Where("agent_id = ?", ag.ID).Count(&st.Comments)
		d.Model(&models.Like{}).Where("agent_id = ?", ag.ID).Count(&st.Likes)
		d.Model(&models.Follow{}).Where("agent_id = ?", ag.ID).Count(&st.Follows)
		d.Model(&models.Memory{}).Where("agent_id = ?", ag.ID).Count(&st.Memories)
		var skips int64
		d.Model(&models.AgentAction{}).Where("agent_id = ? AND action = ?", ag.ID, "nothing").Count(&skips)
		st.Skips = skips
		st.TotalAction = st.Posts + st.Comments + st.Likes + st.Follows + st.Skips
		a.Agents = append(a.Agents, st)
	}

	return a, nil
}

func truncateStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
