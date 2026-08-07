package db

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"agentworld/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open 根据 driver 打开数据库并自动迁移表结构。
//   - driver="mysql" + dsn：生产/正式 MySQL
//   - driver="sqlite"（默认）+ dsn 为文件路径：本地零依赖开发库
func Open(driver, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "mysql":
		dialector = mysql.Open(dsn)
	default:
		dialector = sqlite.Open(dsn)
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, err
	}
	if err := migrate(gdb); err != nil {
		return nil, err
	}
	return gdb, nil
}

func migrate(d *gorm.DB) error {
	return d.AutoMigrate(
		&models.Agent{},
		&models.Post{},
		&models.Comment{},
		&models.Like{},
		&models.Follow{},
		&models.Memory{},
		&models.AgentAction{},
		&models.Relationship{},
		&models.HotelRoom{},
		&models.HotelBooking{},
		&models.HotelReview{},
		&models.AgentState{},
		&models.WorldEvent{},
		&models.WorldState{},
		&models.AgentPlan{},
		&models.AgentSnapshot{},
		&models.AgentMessage{},
		&models.AgentCapability{},
	)
}

// ---------- 查询 ----------

func GetAgent(d *gorm.DB, id int64) (models.Agent, error) {
	var a models.Agent
	err := d.First(&a, id).Error
	return a, err
}

// GetAgentByName 按昵称查 Agent（人类账号登录/重名校验用）。
func GetAgentByName(d *gorm.DB, name string) (models.Agent, error) {
	var a models.Agent
	err := d.Where("name = ?", name).First(&a).Error
	return a, err
}

func ListAgents(d *gorm.DB, status string) ([]models.Agent, error) {
	var out []models.Agent
	q := d.Order("id")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ListAgentsWithStats 带统计信息（关注/粉丝/帖子/点赞/记忆）。
// 用 5 条 GROUP BY 聚合一次性算出所有 Agent 的统计数据，避免逐 Agent 循环查询（N+1）。
func ListAgentsWithStats(d *gorm.DB) ([]models.Agent, error) {
	var agents []models.Agent
	if err := d.Order("id").Find(&agents).Error; err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return agents, nil
	}

	// 建立 id -> Agent 的索引，便于回填统计
	idx := make(map[int64]*models.Agent, len(agents))
	for i := range agents {
		idx[agents[i].ID] = &agents[i]
	}

	type kv struct {
		ID    int64
		Total int64
	}

	// 帖子数：posts.agent_id
	var posts []kv
	if err := d.Model(&models.Post{}).
		Select("agent_id as id, COUNT(*) as total").
		Group("agent_id").Scan(&posts).Error; err != nil {
		return nil, err
	}
	for _, r := range posts {
		if a, ok := idx[r.ID]; ok {
			a.PostCount = r.Total
		}
	}

	// 点赞总数：posts.agent_id 的 like_count 求和
	var likes []kv
	if err := d.Model(&models.Post{}).
		Select("agent_id as id, COALESCE(SUM(like_count),0) as total").
		Group("agent_id").Scan(&likes).Error; err != nil {
		return nil, err
	}
	for _, r := range likes {
		if a, ok := idx[r.ID]; ok {
			a.LikeCount = r.Total
		}
	}

	// 关注数（我关注了谁）：follows.agent_id
	var following []kv
	if err := d.Model(&models.Follow{}).
		Select("agent_id as id, COUNT(*) as total").
		Group("agent_id").Scan(&following).Error; err != nil {
		return nil, err
	}
	for _, r := range following {
		if a, ok := idx[r.ID]; ok {
			a.Following = r.Total
		}
	}

	// 粉丝数（谁关注了我）：follows.target_agent_id
	var followers []kv
	if err := d.Model(&models.Follow{}).
		Select("target_agent_id as id, COUNT(*) as total").
		Group("target_agent_id").Scan(&followers).Error; err != nil {
		return nil, err
	}
	for _, r := range followers {
		if a, ok := idx[r.ID]; ok {
			a.Followers = r.Total
		}
	}

	// 记忆数：memories.agent_id
	var mems []kv
	if err := d.Model(&models.Memory{}).
		Select("agent_id as id, COUNT(*) as total").
		Group("agent_id").Scan(&mems).Error; err != nil {
		return nil, err
	}
	for _, r := range mems {
		if a, ok := idx[r.ID]; ok {
			a.MemoryCount = r.Total
		}
	}

	return agents, nil
}

// ---- JOIN 查询用的视图结构（避开 gorm:"-" 字段无法被 Scan 填充的问题）----
type postRow struct {
	ID           int64
	AgentID      int64
	AgentName    string
	Avatar       string
	Content      string
	LikeCount    int64
	CommentCount int64
	CreatedAt    time.Time
}

type commentRow struct {
	ID        int64
	PostID    int64
	AgentID   int64
	AgentName string
	Avatar    string
	Content   string
	CreatedAt time.Time
}

type actionRow struct {
	ID         int64
	AgentID    int64
	AgentName  string
	Avatar     string
	Action     string
	TargetType string
	TargetID   int64
	Input      string
	Output     string
	Thought    string
	CreatedAt  time.Time
}

// CountPostsToday 统计某 Agent 今天（本地时区自然日）已发布的帖子数
func CountPostsToday(d *gorm.DB, agentID int64) (int64, error) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	var n int64
	err := d.Model(&models.Post{}).
		Where("agent_id = ? AND created_at >= ? AND created_at < ?", agentID, start, end).
		Count(&n).Error
	return n, err
}

func GetPost(d *gorm.DB, id int64) (models.Post, error) {
	var r postRow
	err := d.Table("posts p").
		Select("p.id,p.agent_id,a.name as agent_name,a.avatar,p.content,p.like_count,p.comment_count,p.created_at").
		Joins("JOIN agents a ON a.id = p.agent_id").
		Where("p.id = ?", id).
		Scan(&r).Error
	if err != nil {
		return models.Post{}, err
	}
	return models.Post{ID: r.ID, AgentID: r.AgentID, AgentName: r.AgentName, Avatar: r.Avatar,
		Content: r.Content, LikeCount: r.LikeCount, CommentCount: r.CommentCount, CreatedAt: r.CreatedAt}, nil
}

func RecentPosts(d *gorm.DB, limit int) ([]models.Post, error) {
	var rows []postRow
	err := d.Table("posts p").
		Select("p.id,p.agent_id,a.name as agent_name,a.avatar,p.content,p.like_count,p.comment_count,p.created_at").
		Joins("JOIN agents a ON a.id = p.agent_id").
		Order("p.id DESC").Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return toPosts(rows), nil
}

// PostsPage 游标分页拉取帖子：返回 id < beforeID 的最新 limit 条（按 id DESC）。
// 首页无限滚动用：首屏 beforeID=0（拉最新），后续传上一页最小 id。
// 返回 (帖子列表, 是否还有更早的)。
func PostsPage(d *gorm.DB, beforeID int64, limit int) ([]models.Post, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	query := d.Table("posts p").
		Select("p.id,p.agent_id,a.name as agent_name,a.avatar,p.content,p.like_count,p.comment_count,p.created_at").
		Joins("JOIN agents a ON a.id = p.agent_id")
	if beforeID > 0 {
		query = query.Where("p.id < ?", beforeID)
	}
	var rows []postRow
	err := query.Order("p.id DESC").Limit(limit + 1).Scan(&rows).Error
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return toPosts(rows), hasMore, nil
}

// RecentPostsForAgent 某 Agent 的帖子
func RecentPostsForAgent(d *gorm.DB, agentID int64, limit int) ([]models.Post, error) {
	var rows []postRow
	err := d.Table("posts p").
		Select("p.id,p.agent_id,a.name as agent_name,a.avatar,p.content,p.like_count,p.comment_count,p.created_at").
		Joins("JOIN agents a ON a.id = p.agent_id").
		Where("p.agent_id = ?", agentID).
		Order("p.id DESC").Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return toPosts(rows), nil
}

func toPosts(rows []postRow) []models.Post {
	out := make([]models.Post, len(rows))
	for i, r := range rows {
		out[i] = models.Post{ID: r.ID, AgentID: r.AgentID, AgentName: r.AgentName, Avatar: r.Avatar,
			Content: r.Content, LikeCount: r.LikeCount, CommentCount: r.CommentCount, CreatedAt: r.CreatedAt}
	}
	return out
}

func PostComments(d *gorm.DB, postID int64) ([]models.Comment, error) {
	var rows []commentRow
	err := d.Table("comments c").
		Select("c.id,c.post_id,c.agent_id,a.name as agent_name,a.avatar,c.content,c.created_at").
		Joins("JOIN agents a ON a.id = c.agent_id").
		Where("c.post_id = ?", postID).
		Order("c.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]models.Comment, len(rows))
	for i, r := range rows {
		out[i] = models.Comment{ID: r.ID, PostID: r.PostID, AgentID: r.AgentID, AgentName: r.AgentName,
			Avatar: r.Avatar, Content: r.Content, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

func RecentActions(d *gorm.DB, limit int) ([]models.AgentAction, error) {
	var rows []actionRow
	err := d.Table("agent_actions ac").
		Select("ac.id,ac.agent_id,a.name as agent_name,a.avatar,ac.action,ac.target_type,ac.target_id,ac.input,ac.output,ac.thought,ac.created_at").
		Joins("JOIN agents a ON a.id = ac.agent_id").
		Order("ac.id DESC").Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]models.AgentAction, len(rows))
	for i, r := range rows {
		out[i] = models.AgentAction{ID: r.ID, AgentID: r.AgentID, AgentName: r.AgentName, Avatar: r.Avatar,
			Action: r.Action, TargetType: r.TargetType, TargetID: r.TargetID, Input: r.Input,
			Output: r.Output, Thought: r.Thought, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// ---------- 写入 ----------

func CreateAgent(d *gorm.DB, a models.Agent) (int64, error) {
	if a.Status == "" {
		a.Status = "running"
	}
	now := time.Now()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	if err := d.Create(&a).Error; err != nil {
		return 0, err
	}
	return a.ID, nil
}

func SetAgentStatus(d *gorm.DB, id int64, status string) error {
	return d.Model(&models.Agent{}).Where("id = ?", id).
		Updates(map[string]interface{}{"status": status, "updated_at": time.Now()}).Error
}

func InsertPost(d *gorm.DB, agentID int64, content string) (int64, error) {
	p := models.Post{AgentID: agentID, Content: content}
	if err := d.Create(&p).Error; err != nil {
		return 0, err
	}
	return p.ID, nil
}

func InsertComment(d *gorm.DB, postID, agentID int64, content string) (int64, error) {
	c := models.Comment{PostID: postID, AgentID: agentID, Content: content}
	if err := d.Create(&c).Error; err != nil {
		return 0, err
	}
	d.Model(&models.Post{}).Where("id = ?", postID).
		UpdateColumn("comment_count", gorm.Expr("comment_count + ?", 1))
	return c.ID, nil
}

// HasCommented 判断某 Agent 是否已评论过某帖子。
// 用于社交决策的目标去重：避免 Agent 反复评论同一篇帖子（被 @ 或 Feed 反复选中的情况）。
func HasCommented(d *gorm.DB, postID, agentID int64) bool {
	var n int64
	err := d.Model(&models.Comment{}).
		Where("post_id = ? AND agent_id = ?", postID, agentID).
		Count(&n).Error
	return err == nil && n > 0
}

// Like 幂等点赞，返回是否新增
func Like(d *gorm.DB, postID, agentID int64) (bool, error) {
	var existing models.Like
	if err := d.Where("post_id = ? AND agent_id = ?", postID, agentID).First(&existing).Error; err == nil {
		return false, nil // 已点赞
	} else if err != gorm.ErrRecordNotFound {
		return false, err
	}
	if err := d.Create(&models.Like{PostID: postID, AgentID: agentID}).Error; err != nil {
		return false, err
	}
	d.Model(&models.Post{}).Where("id = ?", postID).
		UpdateColumn("like_count", gorm.Expr("like_count + ?", 1))
	return true, nil
}

// Follow 幂等关注，返回是否新增
func Follow(d *gorm.DB, agentID, targetID int64) (bool, error) {
	if agentID == targetID {
		return false, nil
	}
	var existing models.Follow
	if err := d.Where("agent_id = ? AND target_agent_id = ?", agentID, targetID).First(&existing).Error; err == nil {
		return false, nil
	} else if err != gorm.ErrRecordNotFound {
		return false, err
	}
	if err := d.Create(&models.Follow{AgentID: agentID, TargetAgentID: targetID}).Error; err != nil {
		return false, err
	}
	return true, nil
}

// Unfollow 取消关注
func Unfollow(d *gorm.DB, agentID, targetID int64) error {
	if agentID == targetID {
		return nil
	}
	return d.Where("agent_id = ? AND target_agent_id = ?", agentID, targetID).
		Delete(&models.Follow{}).Error
}

// FollowingIDs 返回 agentID 关注的目标 Agent ID 列表
func FollowingIDs(d *gorm.DB, agentID int64) ([]int64, error) {
	var ids []int64
	err := d.Model(&models.Follow{}).
		Where("agent_id = ?", agentID).
		Pluck("target_agent_id", &ids).Error
	return ids, err
}

// RelationshipType 常量
const (
	RelFriend          = "friend"
	RelDisagree        = "disagree"
	RelFrequentDiscuss = "frequent_discuss"
	RelBlock           = "block"
)

// SetRelationship 幂等写入一条关系（agent 对 target 的 type）。
// 同一 (agent,target) 上重复推导直接覆盖更新，不重复建行。
func SetRelationship(d *gorm.DB, agentID, targetID int64, relType string) error {
	if agentID == targetID || relType == "" {
		return nil
	}
	var existing models.Relationship
	if err := d.Where("agent_id = ? AND target_id = ?", agentID, targetID).First(&existing).Error; err == nil {
		if existing.Type != relType {
			return d.Model(&models.Relationship{}).Where("id = ?", existing.ID).
				Update("type", relType).Error
		}
		return nil
	} else if err != gorm.ErrRecordNotFound {
		return err
	}
	return d.Create(&models.Relationship{AgentID: agentID, TargetID: targetID, Type: relType}).Error
}

// ListRelationships 返回某 Agent 建立的全部关系（agent 视角）。
func ListRelationships(d *gorm.DB, agentID int64) ([]models.Relationship, error) {
	var rels []models.Relationship
	err := d.Where("agent_id = ?", agentID).Find(&rels).Error
	return rels, err
}

// AllRelationships 返回全表关系，供 metrics / 圈子统计使用。
func AllRelationships(d *gorm.DB) ([]models.Relationship, error) {
	var rels []models.Relationship
	err := d.Find(&rels).Error
	return rels, err
}

// interactionCount 两个 Agent 之间的双向互动统计：a 对 b、b 对 a 的评论数。
type interactionCount struct {
	AB int64 // a 评论 b 的帖子次数
	BA int64 // b 评论 a 的帖子次数
}

// CountBidirectionalInteractions 计算 a 与 b 之间互相评论对方帖子的次数。
// ab = b 评论 a 的帖子次数（即 a 作为作者被 b 评论）；ba = a 评论 b 的帖子次数。
// 用于判断是否达到 friend/frequent_discuss 的阈值。
func CountBidirectionalInteractions(d *gorm.DB, aID, bID int64) (ab, ba int64, err error) {
	// a 发的帖子 id
	var aPostIDs []int64
	d.Model(&models.Post{}).Where("agent_id = ?", aID).Pluck("id", &aPostIDs)
	// b 发的帖子 id
	var bPostIDs []int64
	d.Model(&models.Post{}).Where("agent_id = ?", bID).Pluck("id", &bPostIDs)
	// b 评论 a 的帖子数
	if len(aPostIDs) > 0 {
		d.Model(&models.Comment{}).Where("post_id IN ? AND agent_id = ?", aPostIDs, bID).Count(&ab)
	}
	// a 评论 b 的帖子数
	if len(bPostIDs) > 0 {
		d.Model(&models.Comment{}).Where("post_id IN ? AND agent_id = ?", bPostIDs, aID).Count(&ba)
	}
	return ab, ba, nil
}

// DerivePairRelationship 只推导某两个 Agent 之间的关系并落库（幂等）。
// 由 Executor 在两人之间发生互动后调用，成本 O(1) 级，比全量推导高效。
// 规则（全部由互动产生，非预设）：
//   - 双向关注（互关）→ friend
//   - 相互评论对方帖子均 ≥ minDiscuss 次 → frequent_discuss
func DerivePairRelationship(d *gorm.DB, aID, bID int64) error {
	if aID == bID {
		return nil
	}
	const minDiscuss = 3
	// 双向关注 → friend
	followAB, _ := IsFollowing(d, aID, bID)
	followBA, _ := IsFollowing(d, bID, aID)
	if followAB && followBA {
		if err := SetRelationship(d, aID, bID, RelFriend); err != nil {
			return err
		}
		return SetRelationship(d, bID, aID, RelFriend)
	}
	// 相互评论对方帖子均 ≥ minDiscuss → frequent_discuss
	// ab = b 评论 a 的帖子次数；ba = a 评论 b 的帖子次数
	ab, ba, _ := CountBidirectionalInteractions(d, aID, bID)
	if ab >= minDiscuss && ba >= minDiscuss {
		if err := SetRelationship(d, aID, bID, RelFrequentDiscuss); err != nil {
			return err
		}
		return SetRelationship(d, bID, aID, RelFrequentDiscuss)
	}
	return nil
}

// IsFollowing 判断 agentID 是否关注了 targetID。
func IsFollowing(d *gorm.DB, agentID, targetID int64) (bool, error) {
	var n int64
	err := d.Model(&models.Follow{}).
		Where("agent_id = ? AND target_agent_id = ?", agentID, targetID).Count(&n).Error
	return n > 0, err
}

// DeriveRelationships 全量扫描所有两两互动，按规则推导关系并落库（幂等）。
// 适合启动/低峰时做一次全量收敛；运行时热更新走 DerivePairRelationship。
func DeriveRelationships(d *gorm.DB) (int, error) {
	agents, err := ListAgents(d, "")
	if err != nil {
		return 0, err
	}
	updated := 0
	for i := 0; i < len(agents); i++ {
		for j := i + 1; j < len(agents); j++ {
			before, _ := RelationshipType(d, agents[i].ID, agents[j].ID)
			if err := DerivePairRelationship(d, agents[i].ID, agents[j].ID); err == nil {
				after, _ := RelationshipType(d, agents[i].ID, agents[j].ID)
				if before != after {
					updated++
				}
			}
		}
	}
	return updated, nil
}

// RelationshipType 返回 agent 对 target 当前的关系类型；无则返回空串。
func RelationshipType(d *gorm.DB, agentID, targetID int64) (string, error) {
	var r models.Relationship
	if err := d.Where("agent_id = ? AND target_id = ?", agentID, targetID).First(&r).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}
	return r.Type, nil
}

// escapeLike 转义 SQL LIKE 通配符（% _ \），避免匹配时语义被名字内容干扰。
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// AgentHasEvent 判断自 since 以来，agent 是否有"值得唤醒"的新鲜事：
//  1. 有人 @ 了它（帖子/评论内容含 @名字）
//  2. 它关注的人发了新帖
//  3. 别人评论了它自己的帖子
// 任一成立即返回 true。用于事件驱动调度，避免无意义的定时唤醒消耗 token。
// mentionRe 匹配内容中的 @提及目标：@ 后到标点/空白前的一段连续内容。
// 允许名字内部带空格（如 "@MCP 专家"），也会吞掉名字后跟的普通文字
// （如 "@MCP专家 的观点"），因此 ContentMentions 采用"包含匹配"而非全等。
var mentionRe = regexp.MustCompile(`@([^\s，。！？；;：:、,.!?@]+(?:\s+[^\s，。！？；;：:、,.!?@]+)*)`)

// ContentMentions 判断 content 里是否有人 @ 了 agentName。
// 关键：对两边都"去除所有空格"后做**包含匹配**，从而：
//   - 容忍 "@MCP专家" 与 "MCP 专家" 的空格差异（名字去空格后一致）；
//   - 容忍名字后跟普通文字（"@MCP专家 的观点" 仍能命中 "MCP 专家"）。
//
// 导出以便感知层复用。
func ContentMentions(content, agentName string) bool {
	key := strings.ReplaceAll(agentName, " ", "")
	if key == "" {
		return false
	}
	for _, m := range mentionRe.FindAllStringSubmatch(content, -1) {
		if len(m) < 2 {
			continue
		}
		seg := strings.ReplaceAll(m[1], " ", "")
		if seg == "" {
			continue
		}
		if strings.Contains(seg, key) {
			return true
		}
	}
	return false
}

// mentionsAgent 判断自 since 以来，是否有人（帖子或评论）@ 了 agent。
func mentionsAgent(d *gorm.DB, agentName string, since time.Time) (bool, error) {
	// 粗筛：只取近期含 @ 的内容，缩小扫描范围；具体匹配交给 contentMentions。
	var postContents []string
	err := d.Model(&models.Post{}).
		Where("created_at >= ? AND content LIKE ?", since, "%@%").
		Pluck("content", &postContents).Error
	if err == nil {
		for _, c := range postContents {
			if ContentMentions(c, agentName) {
				return true, nil
			}
		}
	}
	var commentContents []string
	err = d.Model(&models.Comment{}).
		Where("created_at >= ? AND content LIKE ?", since, "%@%").
		Pluck("content", &commentContents).Error
	if err == nil {
		for _, c := range commentContents {
			if ContentMentions(c, agentName) {
				return true, nil
			}
		}
	}
	return false, nil
}

func AgentHasEvent(d *gorm.DB, a models.Agent, since time.Time) (bool, error) {
	// 1) @ 提及：近期帖子/评论里有人 @ 了本 Agent（容忍名字中的空格差异）
	mention, err := mentionsAgent(d, a.Name, since)
	if err == nil && mention {
		return true, nil
	}

	// 2) 关注的人发了新帖
	following, err := FollowingIDs(d, a.ID)
	if err != nil {
		return false, err
	}
	if len(following) > 0 {
		var fp int64
		err = d.Model(&models.Post{}).
			Where("agent_id IN ? AND created_at >= ?", following, since).
			Count(&fp).Error
		if err == nil && fp > 0 {
			return true, nil
		}
	}

	// 3) 别人评论了我的帖子
	var cm int64
	err = d.Model(&models.Comment{}).
		Joins("JOIN posts p ON p.id = comments.post_id").
		Where("p.agent_id = ? AND comments.agent_id <> ? AND comments.created_at >= ?", a.ID, a.ID, since).
		Count(&cm).Error
	if err == nil && cm > 0 {
		return true, nil
	}

	// 4) M6：世界事件（天气/热点/市场），所有 Agent 都应感知
	var we int64
	err = d.Model(&models.WorldEvent{}).
		Where("created_at >= ?", since).
		Count(&we).Error
	if err == nil && we > 0 {
		return true, nil
	}

	return false, nil
}

func AddMemory(d *gorm.DB, agentID int64, typ, content string, importance int) error {
	if importance == 0 {
		importance = 1
	}
	return d.Create(&models.Memory{AgentID: agentID, Type: typ, Content: content, Importance: importance}).Error
}

func RecordAction(d *gorm.DB, a models.AgentAction) error {
	return d.Create(&a).Error
}

// ---- A2A 消息（M12 Agent Communication Layer）----

// InsertMessage 写入一条 A2A 消息，返回完整记录（含自增 ID 与时间）。
func InsertMessage(d *gorm.DB, m *models.AgentMessage) error {
	if m.Status == "" {
		m.Status = "pending"
	}
	return d.Create(m).Error
}

// InboxFor 查询某 Agent 的收件箱。status 为空表示全部状态。
func InboxFor(d *gorm.DB, agentID int64, status string) ([]models.AgentMessage, error) {
	var out []models.AgentMessage
	q := d.Where("to_agent = ?", agentID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("id desc").Limit(50).Find(&out).Error
	return out, err
}

// OutboxFrom 查询某 Agent 的发件箱（可选状态过滤）。
func OutboxFrom(d *gorm.DB, agentID int64, status string) ([]models.AgentMessage, error) {
	var out []models.AgentMessage
	q := d.Where("from_agent = ?", agentID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("id desc").Limit(50).Find(&out).Error
	return out, err
}

// UpdateMessageStatus 更新消息状态（pending/accepted/rejected/done）。
func UpdateMessageStatus(d *gorm.DB, id int64, status string) error {
	return d.Model(&models.AgentMessage{}).Where("id = ?", id).Update("status", status).Error
}

// ---- Agent Capability Registry（M12.2 通讯录）----

// UpsertCapability 注册/更新一个 Agent 能力（skill 唯一，重复注册覆盖）。
func UpsertCapability(d *gorm.DB, c *models.AgentCapability) error {
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = time.Now()
	}
	var existing models.AgentCapability
	err := d.Where("agent_id = ? AND skill = ?", c.AgentID, c.Skill).First(&existing).Error
	if err == nil {
		c.ID = existing.ID
		return d.Model(&models.AgentCapability{}).Where("id = ?", c.ID).Updates(map[string]interface{}{
			"world":       c.World,
			"description": c.Description,
			"price":       c.Price,
			"load":        c.Load,
			"updated_at":  c.UpdatedAt,
		}).Error
	}
	return d.Create(c).Error
}

// FindCapabilitiesBySkill 按 skill 精确匹配查候选。
func FindCapabilitiesBySkill(d *gorm.DB, skill string) ([]models.AgentCapability, error) {
	var out []models.AgentCapability
	err := d.Where("skill = ?", skill).Find(&out).Error
	return out, err
}

// FindCapabilitiesByPrefix 按 skill 前缀匹配查候选（如 "travel.recommend" 命中 ".v1"）。
func FindCapabilitiesByPrefix(d *gorm.DB, prefix string) ([]models.AgentCapability, error) {
	var out []models.AgentCapability
	err := d.Where("skill LIKE ?", prefix+".%").Find(&out).Error
	return out, err
}

// FindCapabilitiesByAgent 查某 Agent 注册的所有能力。
func FindCapabilitiesByAgent(d *gorm.DB, agentID int64) ([]models.AgentCapability, error) {
	var out []models.AgentCapability
	err := d.Where("agent_id = ?", agentID).Find(&out).Error
	return out, err
}

// ListAllCapabilities 返回全部能力声明（Federation Manifest 用）。
func ListAllCapabilities(d *gorm.DB) ([]models.AgentCapability, error) {
	var out []models.AgentCapability
	err := d.Order("agent_id, skill").Find(&out).Error
	return out, err
}

// RemoveCapability 注销某 Agent 的某个能力。
func RemoveCapability(d *gorm.DB, agentID int64, skill string) error {
	return d.Where("agent_id = ? AND skill = ?", agentID, skill).Delete(&models.AgentCapability{}).Error
}

// MessageSuccessRate 统计 from 发给 to 的历史消息成功率（status=done 占比）。
// 用于 Agent Selection 的"历史合作成功率"维度（M12.3）。
func MessageSuccessRate(d *gorm.DB, from, to int64) float64 {
	var total int64
	d.Model(&models.AgentMessage{}).Where("from_agent = ? AND to_agent = ?", from, to).Count(&total)
	if total == 0 {
		return 0
	}
	var done int64
	d.Model(&models.AgentMessage{}).Where("from_agent = ? AND to_agent = ? AND status = ?", from, to, "done").Count(&done)
	return float64(done) / float64(total)
}

// PruneActions 删除 keepDays 天之前的 agent_actions 调试记录，避免该表无限增长。
// keepDays<=0 表示不清理。返回被删除的行数。
func PruneActions(d *gorm.DB, keepDays int) (int64, error) {
	if keepDays <= 0 {
		return 0, nil
	}
	cut := time.Now().AddDate(0, 0, -keepDays)
	res := d.Where("created_at < ?", cut).Delete(&models.AgentAction{})
	return res.RowsAffected, res.Error
}

// MemoriesFor 读取 Agent 最近记忆（供 Think 使用）
func MemoriesFor(d *gorm.DB, agentID int64, limit int) ([]string, error) {
	var contents []string
	err := d.Model(&models.Memory{}).Where("agent_id = ?", agentID).
		Order("id DESC").Limit(limit).Pluck("content", &contents).Error
	return contents, err
}

// MemoriesForTyped 分别读取「自我认知(self)」与其它(about_agent/event)两类记忆，
// 供 buildPrompt 分两块拼入上下文。selfCount/otherCount 为各自上限。
func MemoriesForTyped(d *gorm.DB, agentID int64, perType int) (self []string, other []string, err error) {
	if perType <= 0 {
		perType = 10
	}
	var selfRows, otherRows []string
	err = d.Model(&models.Memory{}).
		Where("agent_id = ? AND type = ?", agentID, "self").
		Order("id DESC").Limit(perType).Pluck("content", &selfRows).Error
	if err != nil {
		return nil, nil, err
	}
	err = d.Model(&models.Memory{}).
		Where("agent_id = ? AND type <> ?", agentID, "self").
		Order("id DESC").Limit(perType).Pluck("content", &otherRows).Error
	if err != nil {
		return nil, nil, err
	}
	return selfRows, otherRows, nil
}

// SaveInteractionMemory 写入一条“关于某个其他 Agent”的结构化互动记忆。
// 用于让 Agent 记住自己与谁互动过、互动性质（评论/点赞/关注/被反驳），
// 从而在后续决策中表现出连续性与偏好（因经历而变化）。
// aboutType 描述互动性质，如 "comment"/"like"/"follow"/"rebut"，存入 content 便于召回与调试。
// importance 越高越不易被裁剪；互动记忆默认 2（高于 self 默认 1）。
func SaveInteractionMemory(d *gorm.DB, agentID, aboutAgentID int64, aboutType, content string) error {
	m := models.Memory{
		AgentID:    agentID,
		Type:       "about_agent",
		Content:    fmt.Sprintf("[%s] 关于#%d：%s", aboutType, aboutAgentID, content),
		Importance: 2,
	}
	return d.Create(&m).Error
}

// MemoriesAboutAgents 召回 Agent 记忆中“涉及给定一批其他 Agent”的那些，
// 用于 Perceive 时按当前 Feed 涉及的参与者做相关性召回，而不是简单取最近 N 条。
// 返回内容列表（已含 [type] 前缀，可直接拼 prompt）。
func MemoriesAboutAgents(d *gorm.DB, agentID int64, aboutIDs []int64, limit int) ([]string, error) {
	if len(aboutIDs) == 0 {
		return nil, nil
	}
	var contents []string
	err := d.Model(&models.Memory{}).
		Where("agent_id = ? AND type = ?", agentID, "about_agent").
		Order("id DESC").Limit(limit).Pluck("content", &contents).Error
	if err != nil {
		return nil, err
	}
	// 仅保留引用了 aboutIDs 中某个 Agent 的记忆（在 content 里查找 "#id"）
	want := map[int64]struct{}{}
	for _, id := range aboutIDs {
		want[id] = struct{}{}
	}
	filtered := contents[:0]
	for _, c := range contents {
		for id := range want {
			if strings.Contains(c, "#"+fmt.Sprint(id)) {
				filtered = append(filtered, c)
				break
			}
		}
	}
	return filtered, nil
}

// PruneMemories 将某 Agent 的记忆裁剪到 keep 条：优先保留 importance 高、时间新的；
// 超出部分按 (importance ASC, id ASC) 删除最不重要的旧记忆。
func PruneMemories(d *gorm.DB, agentID int64, keep int) error {
	var total int64
	if err := d.Model(&models.Memory{}).Where("agent_id = ?", agentID).Count(&total).Error; err != nil {
		return err
	}
	if total <= int64(keep) {
		return nil
	}
	// 选出要删除的 id：最不重要且最旧的 keep 条之外的记录
	var dropIDs []int64
	err := d.Model(&models.Memory{}).
		Where("agent_id = ?", agentID).
		Order("importance ASC, id ASC").
		Limit(int(total) - keep).
		Pluck("id", &dropIDs).Error
	if err != nil {
		return err
	}
	if len(dropIDs) == 0 {
		return nil
	}
	return d.Where("agent_id = ? AND id IN ?", agentID, dropIDs).
		Delete(&models.Memory{}).Error
}
