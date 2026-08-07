package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agentworld/internal/agent"
	"agentworld/internal/api"
	"agentworld/internal/bus"
	"agentworld/internal/capability"
	"agentworld/internal/config"
	"agentworld/internal/db"
	"agentworld/internal/federation"
	"agentworld/internal/llm"
	"agentworld/internal/logx"
	"agentworld/internal/scheduler"
	"agentworld/internal/world"
	"agentworld/sdk"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func main() {
	cfg := config.C

	// 日志系统：分级 + 滚动落盘 + 文本/JSON（LOG_LEVEL / LOG_DIR / LOG_FORMAT 可配）
	if err := logx.Setup(cfg.LogLevel, cfg.LogDir); err != nil {
		log.Printf("[logx] 日志初始化失败（继续用 stderr）: %v", err)
	}
	logx.SetFormat(logx.ParseFormat(cfg.LogFormat))
	if cfg.LogMaxSizeMB > 0 {
		logx.SetMaxSize(int64(cfg.LogMaxSizeMB) * 1024 * 1024)
	}

	// 数据库：默认 SQLite 本地文件；DB_DRIVER=mysql + DB_DSN 切换 MySQL
	d, err := db.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	sqlDB, _ := d.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}

	if err := db.SeedIfEmpty(d); err != nil {
		log.Fatalf("seed: %v", err)
	}
	// M8.5：补充 Agent 到目标数（默认 30，可 AGENT_TARGET 覆盖），幂等
	if cfg.AgentTarget > 0 {
		if added, err := db.GenAgentsTo(d, cfg.AgentTarget); err != nil {
			logx.Warnf("补充 Agent 失败: %v", err)
		} else if added > 0 {
			logx.Infof("已补充 %d 个 Agent 到目标 %d", added, cfg.AgentTarget)
		}
	}

	llmClient := llm.New(cfg.LLMBase, cfg.LLMKey, cfg.LLMModel)
	if llmClient.Enabled() {
		logx.Infof("已启用真实 LLM: %s", llmClient.ModelName())
	} else {
		logx.Info("未配置 LLM_API_KEY，使用离线 Mock 决策（Agent 仍可自主行动）")
	}
	if cfg.ConfigPath != "" {
		logx.Infof("已加载配置文件: %s", cfg.ConfigPath)
	} else {
		logx.Info("未找到 config.toml，使用内置默认值（可用环境变量覆盖）")
	}
	logx.Info("当前生效参数：")
	for _, line := range cfg.Dump() {
		logx.Info(line)
	}

	brk := bus.NewBroker()
	rt := agent.NewRuntime(d, llmClient, brk)
	// 显式创建社交模块（而非懒加载），以便配置热点采集
	socialMod := agent.NewSocialModule(rt.SDK(), llmClient)
	// 热点采集：默认开启，从互联网抓热搜作为 Mock 内容源（失败回退内置池）
	if cfg.HotspotEnabled {
		stopHot := socialMod.Hot.Start()
		defer func() { close(stopHot) }()
		logx.Infof("热点采集已启用（每 1h 刷新，当前 %d 条热点）", socialMod.Hot.Count())
	} else {
		socialMod.Hot = agent.NewHotPool(false) // 仅用内置池，不联网
		logx.Info("热点采集已禁用，使用内置内容池")
	}
	rt.RegisterModule("social", socialMod)

	// 第二个世界：酒店（HotelModule），验证 Runtime 多世界共存
	hotelMod := agent.NewHotelModule(rt.SDK(), llmClient)
	if err := hotelMod.OnBoot(rt.SDK()); err != nil {
		logx.Warnf("酒店世界初始化失败: %v", err)
	} else {
		rt.RegisterModule("hotel", hotelMod)
		logx.Info("酒店世界已就绪")
	}

	// M10 Module SDK：加载第三方通过 sdk.RegisterModule 注册的世界模块。
	// 任何 import "agentworld/sdk" 的扩展世界都会在此被调度。
	{
		sdkMods := sdk.LoadSDKModules()
		for _, sm := range sdkMods {
		bridge := rt.RegisterSDKModule(sm)
		if err := bridge.OnBoot(rt.SDK()); err != nil {
				logx.Warnf("SDK 世界 %s 初始化失败: %v", sm.Name(), err)
			} else {
				logx.Infof("SDK 世界已就绪: %s", sm.Name())
			}
		}
	}

	// World Engine（M6）：世界主动变化（时间/天气/热点），Agent 感知并响应。
	// 每 30 现实秒推进一次虚拟世界，时间加速 60 倍（1 秒 = 1 分钟）。
	rt.World = world.NewEngine(d, 30*time.Second, 60)
	stopWorld := rt.World.Start()
	defer func() { close(stopWorld) }()
	logx.Info("World Engine 已启动（时间/天气/热点持续演化）")

	// M9：Capability System —— 让 Agent 连接现实。注册 PMS（酒店门锁房卡）MCP 能力。
	// 配置 PMS_MCP_URL 指向 MCP Streamable HTTP 端点后，Agent 决策出 "tool:xxx" 动作时
	// 会调用真实 PMS 服务（发卡/销卡/查卡）。
	if err := setupPMS(rt, cfg); err != nil {
		logx.Warnf("PMS MCP 能力初始化失败: %v", err)
	} else {
		logx.Infof("PMS 能力已注册（当前 %d 个工具可用）", countCapabilityTools(rt))
	}

	// M9：Weather 能力（Open-Meteo，无需 key）。配置式扩展，不改框架。
	setupWeather(rt, cfg)

	// M8.5：每日快照（长期实验数据）。每 6h 尝试，捕获当天指标到 agent_snapshots 表。
	go func() {
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := db.CaptureSnapshot(d); err != nil {
				logx.Warnf("快照失败: %v", err)
			}
		}
	}()
	// 立即记一次（首次）
	if _, err := db.CaptureSnapshot(d); err != nil {
		logx.Warnf("首次快照失败: %v", err)
	}

	// 框架可插拔：实现 agent.Module 接口后 RegisterModule 注入即可，
	// 一个 Runtime 可同时驱动多个世界（social/hotel/…），按 Agent.World 分派。

	// 调度器（独立 scheduler 包）：batch 随 Agent 数自适应（约 agentCount/8，至少 1）
	interval := config.ParseDuration(cfg.WakeEvery)
	agents, _ := db.ListAgents(d, "")
	agentCount := len(agents)
	batchMin := 1
	batchMax := agentCount / 8
	if batchMax < 1 {
		batchMax = 1
	}
	if batchMax > 15 {
		batchMax = 15 // 单批上限，避免单轮太吵
	}
	sched := scheduler.NewScheduler(rt, interval, batchMin, batchMax)
	sched.SetIdleWakeChance(cfg.IdleWakeChance)
	// 每角色每日发帖上限（默认 10），达到后自动跳过当日后续发帖
	rt.SetDailyPostLimit(cfg.DailyPosts)
	// 自主目标（Goal）驱动：开启后用 Agent 的 Goal 影响行为分布；关闭则回退纯随机，用于对照实验
	rt.SetGoalEnabled(cfg.GoalEnabled)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	// 定期清理 agent_actions 调试表，避免无限增长（默认每 24h 一次，保留最近 7 天）
	if cfg.ActionRetentionDays > 0 {
		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			runPrune := func() {
				if n, err := db.PruneActions(d, cfg.ActionRetentionDays); err != nil {
					logx.Errorf("清理 agent_actions 失败: %v", err)
				} else if n > 0 {
					logx.Infof("已清理 %d 条超过 %d 天的 agent_actions 记录", n, cfg.ActionRetentionDays)
				}
			}
			runPrune() // 启动即执行一次
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runPrune()
				}
			}
		}()
	}

	// M12.2 A2A Registry：为世界 Agent 注册能力（skill），供本地能力寻址与
	// M12.4 Federation Manifest（分布式通讯录）使用。幂等，可重复启动。
	registerWorldCapabilities(rt, d)

	// M12.4 Federation：把本 Runtime 接入分布式 Agent Runtime Network。
	// 开启后暴露 Agent Manifest（分布式通讯录）+ 接收远端消息（由 api router 挂载）。
	if cfg.FederationEnabled {
		// 共享密钥（FEDERATION_SECRET）：所有互联实例需配置相同的值，
		// 用于 HMAC 校验跨实例消息签名，防止公网伪造。未配置则不签名（内网可信场景）。
		transport := federation.NewHTTPTransport(10*time.Second, cfg.FederationSecret)
		rt.Fed = federation.NewClient(transport, cfg.WorldName)
		rt.FedEndpoint = federation.NewEndpoint(rt.A2A, cfg.WorldName, cfg.FederationEndpoint, cfg.FederationSecret)
		if cfg.FederationSecret == "" {
			logx.Warn("federation: 未配置 FEDERATION_SECRET，跨实例消息不签名校验（仅限可信内网）")
		}

		// 启动时自动发现配置的远端实例（FEDERATION_PEERS 逗号分隔），
		// 拉取其 Agent Manifest 填充分布式通讯录。
		for _, peer := range strings.Split(cfg.FederationPeers, ",") {
			peer = strings.TrimSpace(peer)
			if peer == "" {
				continue
			}
			if _, err := rt.Fed.DiscoverRemote(context.Background(), peer); err != nil {
				logx.Warnf("federation: 发现远端 %s 失败: %v", peer, err)
			} else {
				logx.Infof("federation: 已发现远端 %s", peer)
			}
		}
		logx.Infof("Federation 已启用（world=%s, endpoint=%s）", cfg.WorldName, cfg.FederationEndpoint)
	} else {
		logx.Info("Federation 未启用（设置 FEDERATION_ENABLED=true 接入分布式网络）")
	}

	gin.SetMode(gin.ReleaseMode)
	router := api.NewRouter(d, rt, brk)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		logx.Infof("AgentWorld 运行中 → http://localhost:%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// 优雅退出
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logx.Info("正在关闭…")
	cancel()
	shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_ = srv.Shutdown(shutdownCtx)
	logx.Flush() // 排空日志队列，确保尾部日志落盘
	fmt.Println("bye.")
}

// setupPMS 初始化 PMS（酒店门锁房卡）MCP 能力：连接 MCP 服务、拉取工具列表，
// 并以 "pms" 能力注册到 Runtime 的 Capability 注册表。
// 未配置 PMS_MCP_URL 时直接返回 nil（能力保持禁用，不报错）。
func setupPMS(rt *agent.Runtime, cfg config.Config) error {
	if cfg.PMSMCPURL == "" {
		return nil
	}
	headers, err := parseHeaders(cfg.PMSMCPHeaders)
	if err != nil {
		return fmt.Errorf("PMS_MCP_HEADERS 解析失败: %w", err)
	}
	be := capability.NewMCPBackend(cfg.PMSMCPURL, headers)
	tools, err := be.ListTools()
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", cfg.PMSMCPURL, err)
	}
	caps := &capability.Capability{
		Name:  "pms",
		Desc:  "PMS（酒店门锁房卡系统）：发卡、销卡、查房卡状态。",
		Tools: make([]*capability.Tool, 0, len(tools)),
	}
	for i := range tools {
		t := tools[i]
		caps.Tools = append(caps.Tools, &t)
	}
	rt.Capabilities.Register(caps)
	logx.Infof("PMS MCP 能力已注册：%s", cfg.PMSMCPURL)
	return nil
}

// setupWeather 接入 Weather 能力（Open-Meteo 免费 API，无需 key）。
// 走现有 HTTPBackend，仅注册一个新能力，不修改框架。示例：能力可配置式扩展。
func setupWeather(rt *agent.Runtime, cfg config.Config) {
	lat := cfg.WeatherLat
	lon := cfg.WeatherLon
	be := capability.NewHTTPBackend("GET", "https://api.open-meteo.com/v1/forecast?current_weather=true", nil)
	be.ParamMode = capability.ParamModeQuery
	be.ResponseParse = capability.ResponseJSON
	be.Timeout = 10 * time.Second

	caps := &capability.Capability{
		Name: "weather",
		Desc: "实时天气查询：获取指定地点的当前天气（气温/风速/天气代码/昼夜）。",
		Tools: []*capability.Tool{
			{
				Name:        "get_weather",
				Description: "查询指定经纬度的当前天气，返回气温、风速、天气状况等。不传则用默认坐标（北京）。",
				Parameters: []capability.Parameter{
					{Name: "latitude", Type: "number", Description: "纬度，默认 " + fmt.Sprintf("%.4f", lat), Default: lat},
					{Name: "longitude", Type: "number", Description: "经度，默认 " + fmt.Sprintf("%.4f", lon), Default: lon},
				},
				Backend: be,
				Hints:   map[string]bool{},
			},
		},
	}
	rt.Capabilities.Register(caps)
	logx.Infof("Weather 能力已注册（Open-Meteo, 默认坐标 %.4f,%.4f）", lat, lon)
}

// countCapabilityTools 返回注册表中所有能力的工具总数。
func countCapabilityTools(rt *agent.Runtime) int {
	n := 0
	for _, c := range rt.Capabilities.List() {
		n += len(c.Tools)
	}
	return n
}

// registerWorldCapabilities 为世界 Agent 注册 A2A 能力（skill）到 Agent Registry（M12.2）。
// 依据 Agent 的名字/角色映射能力，幂等：重复调用会覆盖更新。
// 这些能力同时被 M12.4 Federation 的 Manifest（/.well-known/agent.json）暴露给远端实例，
// 使其他世界的 Agent 能"按能力发现"本世界的服务 Agent。
func registerWorldCapabilities(rt *agent.Runtime, d *gorm.DB) {
	agents, err := db.ListAgents(d, "")
	if err != nil {
		return
	}
	// 角色关键词 → skill 映射（点分版本格式）。用关键词包含匹配，
	// 兼容"酒店前台小周"、"营收经理小吴"这类带人名的角色名。
	skillByRole := []struct {
		keyword string
		skill   string
		desc    string
	}{
		{"前台", "hotel.booking.v1", "酒店预订/入住/退房服务"},
		{"保洁", "hotel.housekeeping.v1", "客房清洁服务"},
		{"维修", "hotel.maintenance.v1", "酒店设备维修服务"},
		{"营收", "hotel.revenue.v1", "酒店营收数据服务"},
	}
	for _, a := range agents {
		for _, rule := range skillByRole {
			if strings.Contains(a.Name, rule.keyword) {
				_ = rt.A2A.Registry().Register(a.ID, a.World, rule.skill, rule.desc, 0)
				break
			}
		}
	}
}

// parseHeaders 把 JSON 对象字符串解析为请求头 map（空串返回空 map）。
func parseHeaders(s string) (map[string]string, error) {
	out := map[string]string{}
	if s == "" {
		return out, nil
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	for k, v := range raw {
		out[k] = v
	}
	return out, nil
}
