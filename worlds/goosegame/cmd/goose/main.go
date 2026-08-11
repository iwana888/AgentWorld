// 鸭鹅杀独立入口：起一个干净的 AgentWorld Runtime，只跑 goosegame 世界。
//
// 运行方式（在项目根目录）：
//
//	go run ./worlds/goosegame/cmd/goose
//
// 环境变量：
//	GOOSE_DB         游戏数据库路径，默认 goosegame.db（独立于微博世界）
//	GOOSE_INTERVAL   唤醒间隔，默认 5s（游戏节奏快）
//	LLM_API_KEY      （可选）启用 LLM 决策；不配置则 8 个 Agent 走规则 Mock
//	LLM_BASE_URL     （可选）LLM 端点，默认 DeepSeek
//	LLM_MODEL        （可选）模型名
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"

	"agentworld/internal/agent"
	"agentworld/internal/bus"
	"agentworld/internal/db"
	"agentworld/internal/llm"
	"agentworld/internal/models"
	"agentworld/internal/scheduler"
	"agentworld/worlds/goosegame"
)

// 8 个鸭子游戏的社交角色（名字/性格/目标，Agent 的"外表"，与隐藏身份无关）。
var roles = []struct {
	name        string
	personality string
	goal        string
}{
	{"舰长", "冷静理性，善于观察分析", "找出谁是不可信的"},
	{"老猎人", "直觉敏锐，经验丰富", "保护大家，揪出鸭子"},
	{"厨师", "热心肠，乐于助人", "让每个人都感到安全"},
	{"工程师", "专注任务，目标明确", "修好所有设备"},
	{"医生", "谨慎多疑，注重细节", "确保没有人受伤"},
	{"牧师", "沉稳可靠，团结众人", "团结鹅群"},
	{"旅行者", "机敏果断，行动迅速", "尽快完成任务"},
	{"侦探", "观察入微，推理缜密", "揭开真相"},
}

func main() {
	dbPath := envOr("GOOSE_DB", "goosegame.db")
	interval := 5 * time.Second
	if v := os.Getenv("GOOSE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}

	// 独立数据库（不碰微博世界）。Open 会自动建表（AutoMigrate）。
	d, err := db.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("[goose] open db: %v", err)
	}
	sqlDB, _ := d.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}

	// 创建/复用 8 个游戏 Agent（World="goosegame"）。
	agentIDs, names := ensureAgents(d)
	// 每个角色的性格（与 roles 一一对应，Inspector 展示）
	personalities := make([]string, 0, len(roles))
	for _, r := range roles {
		personalities = append(personalities, r.personality)
	}

	// LLM 客户端（可选）：有 LLM_API_KEY 则用 LLM 决策，否则规则 Mock。
	llmClient := llm.New(
		envOr("LLM_BASE_URL", "https://api.deepseek.com/v1"),
		os.Getenv("LLM_API_KEY"),
		envOr("LLM_MODEL", "deepseek-chat"),
	)

	// Runtime + 模块
	brk := bus.NewBroker()
	rt := agent.NewRuntime(d, llmClient, brk)
	mod := goosegame.New(agentIDs, names, personalities, llmClient)
	rt.RegisterModule("goosegame", mod)

	// 有 LLM 则全用 LLM 决策，否则全部规则 Mock。
	useLLM := llmClient.Enabled()
	if useLLM {
		log.Printf("[goose] 已启用 LLM 决策（%s）", llmClient.ModelName())
	} else {
		log.Printf("[goose] 未配置 LLM_API_KEY，8 个 Agent 走规则 Mock（零成本）")
	}
	if err := setAgentsUseLLM(d, agentIDs, useLLM); err != nil {
		log.Printf("[goose] 更新 UseLLM 失败: %v", err)
	}

	// Scheduler：自定义唤醒策略（全部唤醒，游戏每轮所有人都有机会行动）。
	sched := scheduler.NewScheduler(rt, interval, 1, len(agentIDs))
	sched.SetWakePolicy(goosegame.AllWakePolicy{})
	sched.SetIdleWakeChance(1.0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	// AI 社会观察台（M5）：HTTP + SSE，浏览器查看 Agent 社会。
	obsAddr := envOr("GOOSE_OBS_ADDR", ":19090")
	obsSrv := goosegame.NewServer(mod)
	go func() {
		if err := obsSrv.Start(obsAddr); err != nil && err.Error() != "http: Server closed" {
			log.Printf("[goose] 观测服务启动失败: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("[goose] 鸭鹅杀世界已启动（%d 个 Agent，db=%s）", len(agentIDs), dbPath)
	log.Printf("[goose] 身份已随机分配：6 鹅 / 1 鸭 / 1 中立。游戏自动进行，Ctrl+C 停止。")
	log.Printf("[goose] AI 社会观察台: http://localhost:19090（观察 Agent 移动/发言/投票）")

	<-sig
	log.Printf("[goose] 收到退出信号，正在停止…")
}

// ensureAgents 创建或复用 8 个游戏 Agent（World="goosegame"）。
// 按名字幂等：已存在则复用（避免重复创建导致身份重复分配）。
// 返回 AgentID 列表和名字列表（顺序对应 roles）。
func ensureAgents(d *gorm.DB) ([]int64, []string) {
	ids := make([]int64, 0, len(roles))
	names := make([]string, 0, len(roles))
	for _, r := range roles {
		name := "鹅_" + r.name
		a, err := db.GetAgentByName(d, name)
		if err != nil {
			// 不存在 → 创建
			id, cerr := db.CreateAgent(d, models.Agent{
				Name:        name,
				World:       "goosegame",
				Personality: r.personality,
				Goal:        r.goal,
				Kind:        "ai",
				Status:      "running",
			})
			if cerr != nil {
				log.Printf("[goose] 创建 Agent %s 失败: %v", name, cerr)
				continue
			}
			ids = append(ids, id)
			names = append(names, r.name)
		} else {
			ids = append(ids, a.ID)
			names = append(names, r.name)
		}
	}
	if len(ids) != len(roles) {
		log.Printf("[goose] 警告：期望 %d 个 Agent，实际 %d 个", len(roles), len(ids))
	}
	return ids, names
}

// setAgentsUseLLM 批量更新 Agent 的 UseLLM 标志。
func setAgentsUseLLM(d *gorm.DB, ids []int64, use bool) error {
	return d.Model(&models.Agent{}).Where("id IN ?", ids).Update("use_llm", use).Error
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
