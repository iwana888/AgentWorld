// Command report 从 agent_snapshots 表生成 AgentWorld 趋势报告（M8.5）。
// 用法：go run ./cmd/report -db bin/agentworld.db
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"agentworld/internal/db"
)

func main() {
	dbPath := flag.String("db", "agentworld.db", "db path")
	flag.Parse()

	d, err := db.Open("sqlite", *dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败:", err)
		os.Exit(1)
	}
	if sqlDB, e := d.DB(); e == nil {
		defer sqlDB.Close()
	}

	snaps, err := db.ListSnapshots(d)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取快照失败:", err)
		os.Exit(1)
	}
	if len(snaps) == 0 {
		fmt.Println("暂无快照数据。请先让服务运行一天，快照会自动记录。")
		return
	}

	// 按日期排序（已升序）
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Date < snaps[j].Date })

	fmt.Println("==============================================================")
	fmt.Println("   AgentWorld Report #1 — 自主 Agent 世界演化趋势")
	fmt.Printf("   快照区间：%s  ~  %s（共 %d 天）\n", snaps[0].Date, snaps[len(snaps)-1].Date, len(snaps))
	fmt.Println("==============================================================")

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "日期\tAgent\t行为\t帖子\t评论\t赞\t关\t记忆\t好友\t频讨\t话题\t社Need\t知Need\t成Need\t娱Need")
	for _, s := range snaps {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%.0f\t%.0f\t%.0f\t%.0f\n",
			s.Date, s.AgentCount, s.ActionCount, s.PostCount, s.CommentCount, s.LikeCount,
			s.FollowCount, s.MemoryCount, s.RelFriend, s.RelFrequent, s.TopicCount,
			s.NeedSocialAvg, s.NeedKnowledgeAvg, s.NeedAchievementAvg, s.NeedEntAvg)
	}
	w.Flush()

	// 总结：首末对比
	first, last := snaps[0], snaps[len(snaps)-1]
	fmt.Println("==============================================================")
	fmt.Println("   演化总结（首日 → 末日）")
	fmt.Printf("   Agent 数   : %d → %d\n", first.AgentCount, last.AgentCount)
	fmt.Printf("   累计行为   : 首日 %d → 末日日增 %d\n", first.ActionCount, last.ActionCount)
	fmt.Printf("   关系网络   : 好友 %d → %d，频繁讨论 %d → %d\n",
		first.RelFriend, last.RelFriend, first.RelFrequent, last.RelFrequent)
	fmt.Printf("   记忆       : 日增 %d → %d\n", first.MemoryCount, last.MemoryCount)
	fmt.Printf("   话题数     : %d → %d\n", first.TopicCount, last.TopicCount)
	fmt.Printf("   需求均值   : 社交 %.0f→%.0f  求知 %.0f→%.0f\n",
		first.NeedSocialAvg, last.NeedSocialAvg, first.NeedKnowledgeAvg, last.NeedKnowledgeAvg)
	fmt.Println("==============================================================")
}
