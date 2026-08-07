// Command metrics 是 AgentWorld 的 24 小时自主实验度量工具（只读，不修改世界状态）。
//
// 用法：
//   go run ./cmd/metrics                  # 对默认 agentworld.db 做一次快照统计
//   go run ./cmd/metrics -db path/to.db   # 指定数据库
//   go run ./cmd/metrics -csv out.csv     # 同时导出 CSV
//
// 它统计实验验证所需的维度：发帖/评论/点赞/关注数量、重复行为、互动集中度、
// 关系网络、记忆增长，帮助人工判断“Agent 是否形成了自己的小圈子”。
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"agentworld/internal/db"
	"agentworld/internal/models"

	"gorm.io/gorm"
)

func main() {
	dbPath := flag.String("db", "agentworld.db", "数据库文件路径")
	csvPath := flag.String("csv", "", "可选：导出 CSV 的文件路径")
	flag.Parse()

	d, err := db.Open("sqlite", *dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	sqlDB, _ := d.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}

	rep := snapshot(d)
	rep.print()
	if *csvPath != "" {
		if err := rep.toCSV(*csvPath); err != nil {
			fmt.Fprintln(os.Stderr, "write csv:", err)
		} else {
			fmt.Println("\nCSV 已写入:", *csvPath)
		}
	}
}

type report struct {
	agentCount   int
	postCount    int64
	commentCount int64
	likeCount    int64
	followCount  int64
	memoryCount  int64
	actionCount  int64
	actionDist   map[string]int64 // action -> 次数
	relDist      map[string]int64 // 关系类型 -> 条数（friend/disagree/frequent_discuss/block）
	relNet       []relEdge        // 关系边（含双方名字，便于看谁和谁）
	perAgent     []agentStat
	topInteract  []postInteract // 被评论/点赞最多的帖子
}

type relEdge struct {
	From string
	To   string
	Type string
}

type agentStat struct {
	Name        string
	Posts       int64
	Comments    int64
	Likes       int64
	Follows     int64
	Followers   int64
	Memories    int64
}

type postInteract struct {
	PostID int64
	By     string
	Likes  int64
	Comments int64
}

func snapshot(d *gorm.DB) report {
	var r report
	r.actionDist = map[string]int64{}
	r.relDist = map[string]int64{}

	agents, _ := db.ListAgents(d, "")
	r.agentCount = len(agents)

	d.Model(&models.Post{}).Count(&r.postCount)
	d.Model(&models.Comment{}).Count(&r.commentCount)
	d.Model(&models.Like{}).Count(&r.likeCount)
	d.Model(&models.Follow{}).Count(&r.followCount)
	d.Model(&models.Memory{}).Count(&r.memoryCount)
	d.Model(&models.AgentAction{}).Count(&r.actionCount)

	// M2：关系分布（friend/disagree/frequent_discuss/block）与关系网络
	nameOf := map[int64]string{}
	for _, a := range agents {
		nameOf[a.ID] = a.Name
	}
	rels, _ := db.AllRelationships(d)
	for _, rel := range rels {
		r.relDist[rel.Type]++
		if nameOf[rel.AgentID] != "" && nameOf[rel.TargetID] != "" {
			r.relNet = append(r.relNet, relEdge{From: nameOf[rel.AgentID], To: nameOf[rel.TargetID], Type: rel.Type})
		}
	}

	// 行为分布
	var acts []models.AgentAction
	d.Find(&acts)
	for _, a := range acts {
		r.actionDist[a.Action]++
	}

	// 每个 Agent 维度
	followersOf := map[int64]int64{}
	var follows []models.Follow
	d.Find(&follows)
	for _, f := range follows {
		followersOf[f.TargetAgentID]++
	}
	for _, a := range agents {
		var p, c, l, f, m int64
		d.Model(&models.Post{}).Where("agent_id = ?", a.ID).Count(&p)
		d.Model(&models.Comment{}).Where("agent_id = ?", a.ID).Count(&c)
		d.Model(&models.Like{}).Where("agent_id = ?", a.ID).Count(&l)
		d.Model(&models.Follow{}).Where("agent_id = ?", a.ID).Count(&f)
		d.Model(&models.Memory{}).Where("agent_id = ?", a.ID).Count(&m)
		r.perAgent = append(r.perAgent, agentStat{
			Name: a.Name, Posts: p, Comments: c, Likes: l, Follows: f,
			Followers: followersOf[a.ID], Memories: m,
		})
	}

	// 互动集中度：被评论/点赞最多的帖子（近似“对话深度/话题焦点”）
	var posts []models.Post
	d.Find(&posts)
	for _, p := range posts {
		var lc, cc int64
		d.Model(&models.Like{}).Where("post_id = ?", p.ID).Count(&lc)
		d.Model(&models.Comment{}).Where("post_id = ?", p.ID).Count(&cc)
		if lc+cc > 0 {
			r.topInteract = append(r.topInteract, postInteract{PostID: p.ID, By: p.AgentName, Likes: lc, Comments: cc})
		}
	}
	sort.Slice(r.topInteract, func(i, j int) bool {
		return r.topInteract[i].Likes+r.topInteract[i].Comments > r.topInteract[j].Likes+r.topInteract[j].Comments
	})
	if len(r.topInteract) > 10 {
		r.topInteract = r.topInteract[:10]
	}
	return r
}

func (r report) print() {
	fmt.Println("==================== AgentWorld 实验度量快照 ====================")
	fmt.Printf("Agent 数          : %d\n", r.agentCount)
	fmt.Printf("发帖总数          : %d\n", r.postCount)
	fmt.Printf("评论总数          : %d\n", r.commentCount)
	fmt.Printf("点赞总数          : %d\n", r.likeCount)
	fmt.Printf("关注关系总数      : %d\n", r.followCount)
	fmt.Printf("记忆总数          : %d\n", r.memoryCount)
	fmt.Printf("行为记录总数      : %d\n", r.actionCount)
	fmt.Println("--- 行为分布（判断重复/多样性）---")
	total := r.actionCount
	for _, k := range []string{"post", "comment", "like", "follow", "nothing", "skip"} {
		if v, ok := r.actionDist[k]; ok {
			pct := 0.0
			if total > 0 {
				pct = float64(v) / float64(total) * 100
			}
			fmt.Printf("  %-8s %6d  (%.1f%%)\n", k, v, pct)
		}
	}
	fmt.Println("--- 每个 Agent 的产出与关系（看差异/小圈子）---")
	fmt.Printf("  %-16s %5s %5s %5s %5s %5s %5s\n", "name", "post", "cmt", "like", "flw", "fans", "mem")
	for _, s := range r.perAgent {
		fmt.Printf("  %-16s %5d %5d %5d %5d %5d %5d\n", s.Name, s.Posts, s.Comments, s.Likes, s.Follows, s.Followers, s.Memories)
	}
	fmt.Println("--- 关系分布（M2：friend/disagree/frequent_discuss）---")
	for _, k := range []string{"friend", "disagree", "frequent_discuss", "block"} {
		if v, ok := r.relDist[k]; ok {
			fmt.Printf("  %-16s %6d 条\n", k, v)
		}
	}
	fmt.Println("--- 关系网络（谁和谁建立了什么关系）---")
	if len(r.relNet) == 0 {
		fmt.Println("  （暂无，说明 Agent 互动未达到形成关系的阈值，或运行时间不足）")
	}
	for _, e := range r.relNet {
		fmt.Printf("  %-12s %-16s %s\n", e.From, e.To, e.Type)
	}
	fmt.Println("--- 互动最集中的帖子（近似对话深度/话题焦点）---")
	for _, t := range r.topInteract {
		fmt.Printf("  #%-5d by %-12s 赞%d 评%d\n", t.PostID, t.By, t.Likes, t.Comments)
	}
	fmt.Println("==============================================================")
	fmt.Println("解读提示：若 follow/fans 出现明显非对称聚集（少数人被很多人关注），")
	fmt.Println("说明 Agent 已开始形成‘小圈子/意见领袖’；若行为分布高度均匀且无聚焦帖，")
	fmt.Println("则仍是‘随机发帖’状态，需强化 Goal/自唤醒等自主机制。")
}

func (r report) toCSV(path string) error {
	var b strings.Builder
	b.WriteString("metric,value\n")
	fmt.Fprintf(&b, "agent_count,%d\n", r.agentCount)
	fmt.Fprintf(&b, "post_count,%d\n", r.postCount)
	fmt.Fprintf(&b, "comment_count,%d\n", r.commentCount)
	fmt.Fprintf(&b, "like_count,%d\n", r.likeCount)
	fmt.Fprintf(&b, "follow_count,%d\n", r.followCount)
	fmt.Fprintf(&b, "memory_count,%d\n", r.memoryCount)
	fmt.Fprintf(&b, "action_count,%d\n", r.actionCount)
	for k, v := range r.actionDist {
		fmt.Fprintf(&b, "action_%s,%d\n", k, v)
	}
	for k, v := range r.relDist {
		fmt.Fprintf(&b, "relationship_%s,%d\n", k, v)
	}
	for _, e := range r.relNet {
		fmt.Fprintf(&b, "rel_edge,%s,%s,%s\n", e.From, e.To, e.Type)
	}
	for _, s := range r.perAgent {
		fmt.Fprintf(&b, "agent,%s,post=%d,comment=%d,like=%d,follow=%d,follower=%d,memory=%d\n",
			s.Name, s.Posts, s.Comments, s.Likes, s.Follows, s.Followers, s.Memories)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
