// Economy World 独立入口：起一个干净的 AgentWorld Runtime，只跑经济世界。
//
// 运行方式（在项目根目录）：
//	go run ./worlds/economy/cmd/economy
//
// 环境变量：
//	ECO_DB         经济世界数据库路径，默认 economy.db
//	ECO_INTERVAL   唤醒间隔，默认 3s
//	ECO_OBS_ADDR   观察服务地址，默认 :19100
//	ECO_TICK       世界需求刷新间隔，默认 5s（世界自己产生新工作/价格波动）
//	LLM_API_KEY    （可选）启用 LLM 决策；不配置则 20 个 Agent 走规则 Planner
//	LLM_BASE_URL   （可选）LLM 端点
//	LLM_MODEL      （可选）模型名
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
	"agentworld/worlds/economy"
	ec "agentworld/worlds/economy/economy"
	"agentworld/worlds/goosegame/goose"
)

// 经济世界角色（名字/职业/性格/初始资产）由 economy.InitialProfiles 定义。
// 这里只创建 Agent 的持久元数据（名字/职业/性格/目标）。

func main() {
	dbPath := envOr("ECO_DB", "economy.db")
	interval := 3 * time.Second
	if v := os.Getenv("ECO_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			interval = d
		}
	}
	tick := 5 * time.Second
	if v := os.Getenv("ECO_TICK"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			tick = d
		}
	}

	d, err := db.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("[economy] open db: %v", err)
	}
	sqlDB, _ := d.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}

	// 创建/复用 20 个经济 Agent。
	agentIDs, names, personalities := ensureAgents(d)

	llmClient := llm.New(
		envOr("LLM_BASE_URL", "https://api.deepseek.com/v1"),
		os.Getenv("LLM_API_KEY"),
		envOr("LLM_MODEL", "deepseek-chat"),
	)

	brk := bus.NewBroker()
	rt := agent.NewRuntime(d, llmClient, brk)
	// 事件总线（观察台），复用 goosegame 的通用 Observatory。
	obs := goose.NewObservatory(goose.ObservOpts{MaxEvents: 1000})
	mod := economy.New(agentIDs, names, personalities, obs)
	rt.RegisterModule("economy", mod)

	useLLM := llmClient.Enabled()
	if useLLM {
		log.Printf("[economy] 已启用 LLM 决策（%s）", llmClient.ModelName())
	} else {
		log.Printf("[economy] 未配置 LLM_API_KEY，20 个 Agent 走规则 Planner（自主经济决策）")
	}
	_ = setAgentsUseLLM(d, agentIDs, useLLM)

	// Scheduler：全部唤醒。
	sched := scheduler.NewScheduler(rt, interval, 1, len(agentIDs))
	sched.SetWakePolicy(economy.AllWakePolicy{})
	sched.SetIdleWakeChance(1.0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	// 世界需求生成器：世界自己产生新工作/价格波动。
	go func() {
		tk := time.NewTicker(tick)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				mod.Game().RoundTick()
			}
		}
	}()

	// 经济观察台。
	obsAddr := envOr("ECO_OBS_ADDR", ":19100")
	obsSrv := economy.NewServer(mod)
	go func() {
		if err := obsSrv.Start(obsAddr); err != nil && err.Error() != "http: Server closed" {
			log.Printf("[economy] 观测服务启动失败: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("[economy] 经济世界已启动（%d 个 Agent，db=%s）", len(agentIDs), dbPath)
	log.Printf("[economy] 经济观察台: http://localhost%s", obsAddr)
	log.Printf("[economy] 20 个 Agent 自主赚钱/交易/消费。Ctrl+C 停止。")

	<-sig
	log.Printf("[economy] 收到退出信号，正在停止…")
}

// ensureAgents 创建或复用 20 个经济 Agent。
func ensureAgents(d *gorm.DB) ([]int64, []string, []string) {
	ids := make([]int64, 0, len(ec.InitialProfiles))
	names := make([]string, 0, len(ec.InitialProfiles))
	personalities := make([]string, 0, len(ec.InitialProfiles))
	for _, p := range ec.InitialProfiles {
		name := "ECO_" + p.Name
		a, err := db.GetAgentByName(d, name)
		if err != nil {
			id, cerr := db.CreateAgent(d, models.Agent{
				Name:        name,
				World:       "economy",
				Personality: p.Personality,
				Goal:        "赚到更多钱，改善生活",
				Interests:   p.Profession,
				Kind:        "ai",
				Status:      "running",
			})
			if cerr != nil {
				log.Printf("[economy] 创建 Agent %s 失败: %v", name, cerr)
				continue
			}
			ids = append(ids, id)
			names = append(names, p.Name)
			personalities = append(personalities, p.Personality)
		} else {
			ids = append(ids, a.ID)
			names = append(names, p.Name)
			personalities = append(personalities, p.Personality)
		}
	}
	return ids, names, personalities
}

func setAgentsUseLLM(d *gorm.DB, ids []int64, use bool) error {
	return d.Model(&models.Agent{}).Where("id IN ?", ids).Update("use_llm", use).Error
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
