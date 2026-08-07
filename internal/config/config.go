package config

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// C 全局配置（来自配置文件，环境变量可逐条覆盖，均带默认值）
var C = load()

type Config struct {
	Port               string // HTTP 端口
	DBDriver           string // mysql / sqlite
	DBDSN              string // mysql DSN 或 sqlite 文件路径
	LLMKey             string // LLM API Key（空则走离线 Mock）
	LLMBase            string // OpenAI 兼容 Base URL
	LLMModel           string // 模型名
	WakeEvery          string // 唤醒间隔（如 3s / 5m）
	IdleWakeChance     float64 // 无事件 Agent 被保底随机唤醒的概率（0~1）
	DailyPosts         int    // 每角色每日发帖上限（0=不限制）
	GoalEnabled        bool   // 是否启用 Agent 自主目标（Goal）驱动行为（关闭则回退纯随机，用于对照实验）
	AdminPassword      string // 管理员密码
	JWTSecret          string // JWT 签名密钥
	CORSOrigins        string // 允许的跨域来源（逗号分隔），为空=仅同源
	ActionRetentionDays int   // agent_actions 调试表保留天数（0=不清理）
	LogLevel           string // 日志级别：debug / info / warn / error
	LogDir             string // 日志目录（按天滚动；空=仅 stderr）
	LogFormat          string // 日志格式：text / json
	LogMaxSizeMB       int    // 单日志文件大小阈值（MB），超过自动滚动
	HotspotEnabled     bool   // 是否启用热点采集（从互联网抓热搜作为 Mock 内容源）
	AgentTarget        int    // Agent 目标数量（M8.5：启动时补充到该数）
	PMSMCPURL          string // M9：PMS（酒店门锁房卡）MCP 服务地址（Streamable HTTP 端点），空=不启用
	PMSMCPHeaders      string // M9：PMS MCP 额外请求头（JSON 对象，如 {"Authorization":"Bearer x"}）
	WeatherLat         float64 // M9：Weather 能力默认纬度（Open-Meteo，默认北京 39.9042）
	WeatherLon         float64 // M9：Weather 能力默认经度（默认北京 116.4074）
	FederationEnabled  bool   // M12.4：是否启用 Federation（暴露 Manifest / 接收远端消息）
	WorldName          string // M12.4：本世界（实例）名，用于 Agent Manifest
	FederationEndpoint string // M12.4：本实例对外 HTTP 地址（供远端回发消息）
	FederationPeers    string // M12.4：启动时自动发现的远端实例地址（逗号分隔）
	FederationSecret   string // M12.4：实例间共享密钥（HMAC 校验远端消息签名，防伪造）

	ConfigPath string // 实际加载的配置文件路径（日志用）
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func parseInt(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func parseFloat(s string, def float64) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v < 0 || v > 1 {
		return def
	}
	return v
}

func parseBool(s string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return true
	}
	return b
}

// defaultConfig 返回带默认值的配置（未含任何外部来源）
func defaultConfig() Config {
	driver := envOr("DB_DRIVER", "sqlite")
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		if driver == "mysql" {
			dsn = "root:root@tcp(127.0.0.1:3306)/agentworld?charset=utf8mb4&parseTime=True&loc=Local"
		} else {
			dsn = envOr("DB_PATH", "agentworld.db")
		}
	}
	return Config{
		Port:               envOr("PORT", "18080"),
		DBDriver:           driver,
		DBDSN:              dsn,
		LLMKey:             os.Getenv("LLM_API_KEY"),
		LLMBase:            envOr("LLM_BASE_URL", "https://api.deepseek.com/v1"),
		LLMModel:           envOr("LLM_MODEL", "deepseek-chat"),
		WakeEvery:          envOr("WAKE_INTERVAL", "30s"),
		IdleWakeChance:     parseFloat(envOr("IDLE_WAKE_CHANCE", "0.15"), 0.15),
		DailyPosts:         parseInt(envOr("DAILY_POST_LIMIT", "10")),
		GoalEnabled:        parseBool(envOr("GOAL_ENABLED", "true")),
		AdminPassword:      envOr("ADMIN_PASSWORD", "admin123"),
		JWTSecret:          envOr("JWT_SECRET", "agentworld-secret-key-change-me"),
		CORSOrigins:        os.Getenv("CORS_ORIGINS"),
		ActionRetentionDays: parseInt(envOr("ACTION_RETENTION_DAYS", "7")),
		LogLevel:            envOr("LOG_LEVEL", "info"),
		LogDir:              envOr("LOG_DIR", "logs"),
		LogFormat:           envOr("LOG_FORMAT", "text"),
		LogMaxSizeMB:        parseInt(envOr("LOG_MAX_SIZE_MB", "10")),
		HotspotEnabled:      parseBool(envOr("HOTSPOT_ENABLED", "true")),
		AgentTarget:         parseInt(envOr("AGENT_TARGET", "30")),
		PMSMCPURL:           os.Getenv("PMS_MCP_URL"),
		PMSMCPHeaders:       os.Getenv("PMS_MCP_HEADERS"),
		WeatherLat:          parseFloat(envOr("WEATHER_LAT", "39.9042"), 39.9042),
		WeatherLon:          parseFloat(envOr("WEATHER_LON", "116.4074"), 116.4074),
		FederationEnabled:   parseBool(envOr("FEDERATION_ENABLED", "false")),
		WorldName:           envOr("WORLD_NAME", "default-world"),
		FederationEndpoint:  os.Getenv("FEDERATION_ENDPOINT"),
		FederationPeers:     os.Getenv("FEDERATION_PEERS"),
		FederationSecret:    os.Getenv("FEDERATION_SECRET"),
	}
}

// load 优先级：默认值 < config.toml < 环境变量
func load() Config {
	cfg := defaultConfig()

	// 1) 读取同目录下的 config.toml（如果存在）
	path := findConfigFile()
	if path != "" {
		cfg.ConfigPath = path
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("[config] 读取配置文件 %s 失败: %v（回退默认+环境变量）", path, err)
		} else {
			var fc fileConfig
			if err := toml.Unmarshal(data, &fc); err != nil {
				// 兜底：许多 Windows 记事本保存为 ANSI/GBK，中文注释不是合法 UTF-8，
				// go-toml 会报 "invalid character in comment"。尝试转 UTF-8 再解析。
				if conv, cerr := gbkToUTF8(data); cerr == nil {
					if err2 := toml.Unmarshal(conv, &fc); err2 == nil {
						log.Printf("[config] 配置文件 %s 非 UTF-8 编码，已按 GBK 转码后加载成功", path)
						cfg = applyFile(cfg, fc)
						goto cfgApplied
					}
				}
				log.Printf("[config] 解析配置文件 %s 失败: %v（请检查 TOML 语法；建议用 UTF-8 保存）", path, err)
			} else {
				cfg = applyFile(cfg, fc)
			}
		}
	cfgApplied:
	}

	// 2) 环境变量覆盖（便于容器/临时调参，单条优先级最高）
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("DB_DRIVER"); v != "" {
		cfg.DBDriver = v
	}
	if v := os.Getenv("DB_DSN"); v != "" {
		cfg.DBDSN = v
	}
	if v := os.Getenv("DB_PATH"); v != "" && cfg.DBDriver == "sqlite" {
		cfg.DBDSN = v
	}
	if v := os.Getenv("LLM_API_KEY"); v != "" {
		cfg.LLMKey = v
	}
	if v := os.Getenv("LLM_BASE_URL"); v != "" {
		cfg.LLMBase = v
	}
	if v := os.Getenv("LLM_MODEL"); v != "" {
		cfg.LLMModel = v
	}
	if v := os.Getenv("WAKE_INTERVAL"); v != "" {
		cfg.WakeEvery = v
	}
	if v := os.Getenv("IDLE_WAKE_CHANCE"); v != "" {
		cfg.IdleWakeChance = parseFloat(v, cfg.IdleWakeChance)
	}
	if v := os.Getenv("DAILY_POST_LIMIT"); v != "" {
		cfg.DailyPosts = parseInt(v)
	}
	if v := os.Getenv("GOAL_ENABLED"); v != "" {
		cfg.GoalEnabled = parseBool(v)
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" {
		cfg.AdminPassword = v
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("CORS_ORIGINS"); v != "" {
		cfg.CORSOrigins = v
	}
	if v := os.Getenv("ACTION_RETENTION_DAYS"); v != "" {
		cfg.ActionRetentionDays = parseInt(v)
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("LOG_DIR"); v != "" {
		cfg.LogDir = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("LOG_MAX_SIZE_MB"); v != "" {
		cfg.LogMaxSizeMB = parseInt(v)
	}
	if v := os.Getenv("HOTSPOT_ENABLED"); v != "" {
		cfg.HotspotEnabled = parseBool(v)
	}
	if v := os.Getenv("AGENT_TARGET"); v != "" {
		cfg.AgentTarget = parseInt(v)
	}
	if v := os.Getenv("PMS_MCP_URL"); v != "" {
		cfg.PMSMCPURL = v
	}
	if v := os.Getenv("PMS_MCP_HEADERS"); v != "" {
		cfg.PMSMCPHeaders = v
	}
	if v := os.Getenv("FEDERATION_ENABLED"); v != "" {
		cfg.FederationEnabled = parseBool(v)
	}
	if v := os.Getenv("WORLD_NAME"); v != "" {
		cfg.WorldName = v
	}
	if v := os.Getenv("FEDERATION_ENDPOINT"); v != "" {
		cfg.FederationEndpoint = v
	}
	if v := os.Getenv("FEDERATION_PEERS"); v != "" {
		cfg.FederationPeers = v
	}
	if v := os.Getenv("FEDERATION_SECRET"); v != "" {
		cfg.FederationSecret = v
	}

	// 安全加固：禁止 JWT 使用内置默认密钥上线（否则任何知道默认值者可伪造 admin token）。
	// 若未显式配置 JWT_SECRET（仍为默认值），则启动时随机生成一个，保证每次部署唯一。
	const defaultSecret = "agentworld-secret-key-change-me"
	if cfg.JWTSecret == defaultSecret {
		cfg.JWTSecret = randomSecret(32)
		log.Printf("[config] 警告：JWT_SECRET 未配置，已随机生成（本次会话有效）。生产环境请显式设置 JWT_SECRET 以保持重启后 token 稳定。")
	}

	return cfg
}

// randomSecret 用 crypto/rand 生成 base64 编码的随机密钥，用于未配置时兜底。
func randomSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败几乎不可能；兜底用时间戳+随机，保证非默认值即可。
		return "tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// fileConfig 是 config.toml 的映射结构，字段均可选
type fileConfig struct {
	Port                string `toml:"port"`
	DBDriver            string `toml:"db_driver"`
	DBDSN               string `toml:"db_dsn"`
	DBPath              string `toml:"db_path"`
	LLMKey              string `toml:"llm_api_key"`
	LLMBase             string `toml:"llm_base_url"`
	LLMModel            string `toml:"llm_model"`
	WakeEvery           string `toml:"wake_interval"`
	IdleWakeChance      *float64 `toml:"idle_wake_chance"`
	DailyPosts          *int   `toml:"daily_post_limit"`
	GoalEnabled         *bool  `toml:"goal_enabled"`
	AdminPassword       string `toml:"admin_password"`
	JWTSecret           string `toml:"jwt_secret"`
	CORSOrigins         string `toml:"cors_origins"`
	ActionRetentionDays *int   `toml:"action_retention_days"`
	LogLevel            string `toml:"log_level"`
	LogDir              string `toml:"log_dir"`
	LogFormat           string `toml:"log_format"`
	LogMaxSizeMB        int    `toml:"log_max_size_mb"`
	HotspotEnabled      *bool  `toml:"hotspot_enabled"`
	AgentTarget         *int   `toml:"agent_target"`
	PMSMCPURL           string  `toml:"pms_mcp_url"`
	PMSMCPHeaders       string  `toml:"pms_mcp_headers"`
	WeatherLat          *float64 `toml:"weather_lat"`
	WeatherLon          *float64 `toml:"weather_lon"`
	FederationEnabled   *bool    `toml:"federation_enabled"`
	WorldName           string   `toml:"world_name"`
	FederationEndpoint  string   `toml:"federation_endpoint"`
	FederationPeers     string   `toml:"federation_peers"`
	FederationSecret    string   `toml:"federation_secret"`
}

func applyFile(cfg Config, fc fileConfig) Config {
	if fc.Port != "" {
		cfg.Port = fc.Port
	}
	if fc.DBDriver != "" {
		cfg.DBDriver = fc.DBDriver
	}
	if fc.DBDSN != "" {
		cfg.DBDSN = fc.DBDSN
	} else if fc.DBPath != "" && cfg.DBDriver == "sqlite" {
		cfg.DBDSN = fc.DBPath
	}
	if fc.LLMKey != "" {
		cfg.LLMKey = fc.LLMKey
	}
	if fc.LLMBase != "" {
		cfg.LLMBase = fc.LLMBase
	}
	if fc.LLMModel != "" {
		cfg.LLMModel = fc.LLMModel
	}
	if fc.WakeEvery != "" {
		cfg.WakeEvery = fc.WakeEvery
	}
	if fc.IdleWakeChance != nil {
		if *fc.IdleWakeChance >= 0 && *fc.IdleWakeChance <= 1 {
			cfg.IdleWakeChance = *fc.IdleWakeChance
		}
	}
	if fc.DailyPosts != nil {
		cfg.DailyPosts = *fc.DailyPosts
	}
	if fc.GoalEnabled != nil {
		cfg.GoalEnabled = *fc.GoalEnabled
	}
	if fc.AdminPassword != "" {
		cfg.AdminPassword = fc.AdminPassword
	}
	if fc.JWTSecret != "" {
		cfg.JWTSecret = fc.JWTSecret
	}
	if fc.CORSOrigins != "" {
		cfg.CORSOrigins = fc.CORSOrigins
	}
	if fc.ActionRetentionDays != nil {
		cfg.ActionRetentionDays = *fc.ActionRetentionDays
	}
	if fc.LogLevel != "" {
		cfg.LogLevel = fc.LogLevel
	}
	if fc.LogDir != "" {
		cfg.LogDir = fc.LogDir
	}
	if fc.LogFormat != "" {
		cfg.LogFormat = fc.LogFormat
	}
	if fc.LogMaxSizeMB != 0 {
		cfg.LogMaxSizeMB = fc.LogMaxSizeMB
	}
	if fc.HotspotEnabled != nil {
		cfg.HotspotEnabled = *fc.HotspotEnabled
	}
	if fc.AgentTarget != nil {
		cfg.AgentTarget = *fc.AgentTarget
	}
	if fc.PMSMCPURL != "" {
		cfg.PMSMCPURL = fc.PMSMCPURL
	}
	if fc.PMSMCPHeaders != "" {
		cfg.PMSMCPHeaders = fc.PMSMCPHeaders
	}
	if fc.WeatherLat != nil {
		cfg.WeatherLat = *fc.WeatherLat
	}
	if fc.WeatherLon != nil {
		cfg.WeatherLon = *fc.WeatherLon
	}
	if fc.FederationEnabled != nil {
		cfg.FederationEnabled = *fc.FederationEnabled
	}
	if fc.WorldName != "" {
		cfg.WorldName = fc.WorldName
	}
	if fc.FederationEndpoint != "" {
		cfg.FederationEndpoint = fc.FederationEndpoint
	}
	if fc.FederationPeers != "" {
		cfg.FederationPeers = fc.FederationPeers
	}
	if fc.FederationSecret != "" {
		cfg.FederationSecret = fc.FederationSecret
	}
	return cfg
}

// maskSecret 对密钥类字段做打码，仅保留首尾若干字符，便于日志查看而不泄露
func maskSecret(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return "(空)"
	}
	if len(r) <= 4 {
		return "****"
	}
	return string(r[:2]) + "****" + string(r[len(r)-2:])
}

// Dump 返回当前生效的全部配置（密钥打码），供启动时打印
func (c Config) Dump() []string {
	cors := c.CORSOrigins
	if cors == "" {
		cors = "(仅同源)"
	}
	llm := "(离线 Mock)"
	if c.LLMKey != "" {
		llm = c.LLMModel + " @ " + c.LLMBase
	}
	src := c.ConfigPath
	if src == "" {
		src = "(无，使用默认+环境变量)"
	}
	lines := []string{
		"  配置来源        : " + src,
		"  port            : " + c.Port,
		"  db_driver       : " + c.DBDriver,
		"  db_dsn          : " + c.DBDSN,
		"  llm             : " + llm,
		"  llm_api_key     : " + maskSecret(c.LLMKey),
		"  wake_interval   : " + c.WakeEvery,
		"  idle_wake_chance: " + strconv.FormatFloat(c.IdleWakeChance, 'f', 2, 64),
		"  daily_post_limit: " + itoa(c.DailyPosts),
		"  goal_enabled     : " + strconv.FormatBool(c.GoalEnabled),
		"  admin_password  : " + maskSecret(c.AdminPassword),
		"  jwt_secret      : " + maskSecret(c.JWTSecret),
		"  cors_origins    : " + cors,
		"  action_retention: " + itoa(c.ActionRetentionDays) + " 天",
		"  log_level       : " + c.LogLevel,
		"  log_dir         : " + c.LogDir,
		"  log_format      : " + c.LogFormat,
		"  log_max_size    : " + itoa(c.LogMaxSizeMB) + " MB",
		"  hotspot_enabled : " + strconv.FormatBool(c.HotspotEnabled),
		"  agent_target    : " + itoa(c.AgentTarget),
	}
	if c.FederationEnabled {
		lines = append(lines,
			"  federation      : enabled",
			"  world_name      : "+c.WorldName,
			"  fed_endpoint    : "+c.FederationEndpoint,
			"  fed_peers       : "+c.FederationPeers,
		)
	} else {
		lines = append(lines, "  federation      : disabled")
	}
	if c.PMSMCPURL != "" {
		lines = append(lines,
			"  pms_mcp_url     : "+c.PMSMCPURL,
			"  pms_mcp_headers : "+maskSecret(c.PMSMCPHeaders),
		)
	} else {
		lines = append(lines, "  pms_mcp_url     : (未配置，PMS 能力禁用)")
	}
	return lines
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// gbkToUTF8 将疑似 GBK/GB18030 编码的字节转为 UTF-8；非 GBK 时返回原值与错误
func gbkToUTF8(b []byte) ([]byte, error) {
	dst, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), b)
	return dst, err
}

// findConfigFile 优先使用环境变量 AGENTWORLD_CONFIG 指定的路径；
// 否则在 exe 同目录、当前工作目录查找 config.toml。
func findConfigFile() string {
	if p := os.Getenv("AGENTWORLD_CONFIG"); p != "" {
		return p
	}
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.toml"))
	}
	candidates = append(candidates, "config.toml")
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// LLMEnabled 是否配置了真实 LLM
func (c Config) LLMEnabled() bool { return c.LLMKey != "" }

// ParseDuration 解析唤醒间隔，非法值回退 3s
func ParseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 3 * time.Second
	}
	return d
}
