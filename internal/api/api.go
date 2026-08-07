package api

import (
	"agentworld/internal/agent"
	"agentworld/internal/bus"
	"agentworld/internal/config"
	"agentworld/internal/db"
	"agentworld/internal/logx"
	"agentworld/internal/models"
	"agentworld/webstatic"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// NewRouter 构建 HTTP 路由：/api/* 业务接口 + SSE 活动流 + 静态前端
func NewRouter(d *gorm.DB, rt *agent.Runtime, brk *bus.Broker) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery()) // 保留 panic 恢复
	r.Use(logxRequest())  // 请求日志走 logx（落盘 + 分级）
	r.Use(corsMiddleware())

	// ---- 公开接口（无需登录） ----
	r.GET("/api/feed", func(c *gin.Context) {
		// 游标分页：before_id = 上一页最小 id（首屏 0），limit = 每页条数
		beforeID, _ := strconv.ParseInt(c.DefaultQuery("before_id", "0"), 10, 64)
		limit := 20
		if v, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && v > 0 && v <= 50 {
			limit = v
		}
		posts, hasMore, err := db.PostsPage(d, beforeID, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 返回 {posts, has_more}，前端据此决定是否继续加载
		c.JSON(200, gin.H{"posts": posts, "has_more": hasMore})
	})

	r.GET("/api/agents", func(c *gin.Context) {
		agents, err := db.ListAgentsWithStats(d)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, agents)
	})

	r.GET("/api/agents/:id", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		a, err := db.GetAgent(d, id)
		if err != nil {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		posts, _ := db.RecentPostsForAgent(d, id, 20)
		c.JSON(200, gin.H{"agent": a, "posts": posts})
	})

	// 单帖详情（前台文章详情页用）
	r.GET("/api/posts/:id", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		p, err := db.GetPost(d, id)
		if err != nil || p.ID == 0 {
			c.JSON(404, gin.H{"error": "post not found"})
			return
		}
		c.JSON(200, p)
	})

	// ---- M9：Capability System（能力系统）----
	// 列出全部已注册能力及其工具。
	r.GET("/api/capabilities", func(c *gin.Context) {
		if rt.Capabilities == nil {
			c.JSON(200, gin.H{"capabilities": []interface{}{}})
			return
		}
		c.Data(200, "application/json", []byte(rt.Capabilities.JSON()))
	})

	// 调用一个能力工具（测试/调试用）。
	// body: {"capability":"pms","tool":"read_room_key","arguments":{"roomNumber":"104","lockNumber":"104"}}
	r.POST("/api/capabilities/call", func(c *gin.Context) {
		if rt.Capabilities == nil || rt.Capabilities.Count() == 0 {
			c.JSON(400, gin.H{"error": "未注册任何 Capability"})
			return
		}
		var body struct {
			Capability string                 `json:"capability"`
			Tool       string                 `json:"tool"`
			Arguments  map[string]interface{} `json:"arguments"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": "请求体格式错误: " + err.Error()})
			return
		}
		capObj := rt.Capabilities.Get(body.Capability)
		if capObj == nil {
			c.JSON(404, gin.H{"error": "未知能力 " + body.Capability})
			return
		}
		tool := capObj.FindTool(body.Tool)
		if tool == nil {
			c.JSON(404, gin.H{"error": "未知工具 " + body.Tool})
			return
		}
		out, err := tool.Execute(body.Arguments)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"result": out})
	})

	// ---- 人类身份接口（Phase 3：Human enters） ----
	// 注册人类账号：创建一个 kind=human 的 Agent，可用自己的身份发帖/评论/关注，
	// 且不会被 Scheduler 自主唤醒（仅由人类自己操作）。
	r.POST("/api/humans", func(c *gin.Context) {
		var req struct {
			Name     string `json:"name"`
			Avatar   string `json:"avatar"`
			Password string `json:"password"`
			Bio      string `json:"bio"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			c.JSON(400, gin.H{"error": "昵称不能为空"})
			return
		}
		if len(req.Password) < 4 {
			c.JSON(400, gin.H{"error": "密码至少 4 位"})
			return
		}
		// 校验昵称不重复
		if _, err := db.GetAgentByName(d, name); err == nil {
			c.JSON(409, gin.H{"error": "该昵称已被占用"})
			return
		}
		id, err := db.CreateAgent(d, models.Agent{
			Name:      name,
			Avatar:    req.Avatar,
			Bio:       req.Bio,
			Kind:      "human",
			Password:  req.Password,
			Status:    "running", // human 不受 Scheduler 唤醒，这里只是标记存在
		})
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"id": id})
	})

	// 人类账号登录：校验密码，签发与 admin 同套 HMAC token
	r.POST("/api/humans/login", func(c *gin.Context) {
		var req struct {
			Name     string `json:"name"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		a, err := db.GetAgentByName(d, req.Name)
		if err != nil || a.Kind == "" || a.Kind == "ai" {
			c.JSON(401, gin.H{"error": "账号不存在"})
			return
		}
		if a.Password == "" || a.Password != req.Password {
			c.JSON(401, gin.H{"error": "密码错误"})
			return
		}
		p := tokenPayload{
			User:  a.Name,
			ExpAt: time.Now().Add(24 * time.Hour).Unix(),
		}
		token, err := signToken(p)
		if err != nil {
			c.JSON(500, gin.H{"error": "签发失败"})
			return
		}
		c.SetCookie("aw_human_token", token, 86400, "/", "", false, true)
		c.JSON(200, gin.H{"ok": true, "id": a.ID, "name": a.Name})
	})

	// 人类账号退出
	r.POST("/api/humans/logout", func(c *gin.Context) {
		c.SetCookie("aw_human_token", "", -1, "/", "", false, true)
		c.JSON(200, gin.H{"ok": true})
	})

	// ---- 管理员接口（需要登录） ----
	adminGroup := r.Group("/api/admin")
	adminGroup.POST("/login", adminLogin)
	adminGroup.GET("/check", adminCheck)
	adminGroup.POST("/logout", adminLogout)
	// 世界数据分析：行为/关系/互动焦点/Agent 画像
	adminGroup.GET("/analytics", func(c *gin.Context) {
		ana, err := db.GetAnalytics(d)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, ana)
	})

	admin := r.Group("")
	admin.Use(AuthMiddleware())
	{
		admin.POST("/api/agents", func(c *gin.Context) {
			var a models.Agent
			if err := c.ShouldBindJSON(&a); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			if a.SystemPrompt == "" {
				a.SystemPrompt = defaultPromptFor(a)
			}
			if a.Status == "" {
				a.Status = "running"
			}
			if a.Model == "" {
				a.Model = "DeepSeek"
			}
			if a.Kind == "" {
				a.Kind = "ai"
			}
			id, err := db.CreateAgent(d, a)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, gin.H{"id": id})
		})
	}

	// ---- 写接口（需登录 admin，防公网被滥发/烧钱） ----
	write := r.Group("").Use(AuthMiddleware())

	write.POST("/api/agents/:id/start", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		_ = db.SetAgentStatus(d, id, "running")
		c.JSON(200, gin.H{"ok": true})
	})
	write.POST("/api/agents/:id/stop", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		_ = db.SetAgentStatus(d, id, "paused")
		c.JSON(200, gin.H{"ok": true})
	})

	// Agent 的长期记忆（内心独白），供前端记忆面板展示
	r.GET("/api/agents/:id/memories", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		var mems []models.Memory
		if err := d.Where("agent_id = ?", id).Order("importance DESC, id DESC").Find(&mems).Error; err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, mems)
	})

	r.GET("/api/posts/:id/comments", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		comments, err := db.PostComments(d, id)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, comments)
	})

	write.POST("/api/posts/:id/like", func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		// 演示用：随机选一个 running agent 执行点赞
		agents, _ := db.ListAgents(d, "running")
		if len(agents) == 0 {
			c.JSON(400, gin.H{"error": "no agent"})
			return
		}
		added, err := db.Like(d, id, agents[0].ID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"added": added})
	})

	// ---- 人类参与的写接口（以某个 Agent 身份执行） ----

	// 人类发帖：以 agent_id 身份发布一条动态
	write.POST("/api/posts", func(c *gin.Context) {
		var req struct {
			AgentID int64  `json:"agent_id"`
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		content := strings.TrimSpace(req.Content)
		if content == "" {
			c.JSON(400, gin.H{"error": "内容不能为空"})
			return
		}
		if _, err := db.GetAgent(d, req.AgentID); err != nil {
			c.JSON(404, gin.H{"error": "agent not found"})
			return
		}
		pid, err := db.InsertPost(d, req.AgentID, content)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		_ = db.AddMemory(d, req.AgentID, "self", content, 2)
		publishHumanEvent(brk, d, req.AgentID, "post", "POST", content)
		recordHumanAction(d, req.AgentID, "post", "post", pid, content)
		c.JSON(200, gin.H{"id": pid})
	})

	// 人类评论：以 agent_id 身份评论某帖
	write.POST("/api/posts/:id/comments", func(c *gin.Context) {
		pid, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		var req struct {
			AgentID int64  `json:"agent_id"`
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		content := strings.TrimSpace(req.Content)
		if content == "" {
			c.JSON(400, gin.H{"error": "内容不能为空"})
			return
		}
		if _, err := db.GetAgent(d, req.AgentID); err != nil {
			c.JSON(404, gin.H{"error": "agent not found"})
			return
		}
		cid, err := db.InsertComment(d, pid, req.AgentID, content)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		publishHumanEvent(brk, d, req.AgentID, "comment", "COMMENT", content)
		recordHumanAction(d, req.AgentID, "comment", "post", pid, content)
		c.JSON(200, gin.H{"id": cid})
	})

	// 人类关注：以 agent_id 身份关注目标 agent
	write.POST("/api/agents/:id/follow", func(c *gin.Context) {
		target, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		var req struct {
			AgentID int64 `json:"agent_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if req.AgentID == target {
			c.JSON(400, gin.H{"error": "不能关注自己"})
			return
		}
		added, err := db.Follow(d, req.AgentID, target)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if added {
			publishHumanEvent(brk, d, req.AgentID, "follow", "FOLLOW", fmt.Sprintf("关注了 Agent #%d", target))
			recordHumanAction(d, req.AgentID, "follow", "agent", target, "")
		}
		c.JSON(200, gin.H{"added": added})
	})

	// 人类取消关注
	write.DELETE("/api/agents/:id/follow", func(c *gin.Context) {
		target, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		var req struct {
			AgentID int64 `json:"agent_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if err := db.Unfollow(d, req.AgentID, target); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	r.GET("/api/activity", func(c *gin.Context) {
		acts, err := db.RecentActions(d, 50)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, acts)
	})

	// SSE 实时活动流
	r.GET("/api/stream", streamHandler(brk))

	// M12.4 Federation：暴露 Agent Manifest（分布式通讯录）+ 接收远端消息。
	// 这两个端点由 runtime 持有，gin 桥接标准 handler。
	if rt != nil && rt.FedEndpoint != nil {
		r.GET("/.well-known/agent.json", func(c *gin.Context) {
			rt.FedEndpoint.HandleManifest(c.Writer, c.Request)
		})
		r.POST("/api/federation/messages", func(c *gin.Context) {
			rt.FedEndpoint.HandleMessage(c.Writer, c.Request)
		})
	}

	// 静态前端（web 目录），非 /api 路径回退到文件
	r.NoRoute(serveWeb)

	return r
}

// logxRequest 请求日志中间件：记录方法/路径/状态/耗时，走 logx（落盘+分级）。
// 静态资源请求量大，仅记 /api 接口，避免刷屏。
func logxRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			latency := time.Since(start)
			logx.D("http", logx.F{
				"method": c.Request.Method,
				"path":   c.Request.URL.Path,
				"status": c.Writer.Status(),
				"ms":     latency.Milliseconds(),
			})
		}
	}
}

// corsMiddleware 受限跨域：默认仅允许同源；若配置了 CORS_ORIGINS（逗号分隔）
// 则仅放行这些来源。不开放 *，避免任意网站跨域调用写接口。
func corsMiddleware() gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range strings.Split(config.C.CORSOrigins, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowed[o] = true
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			// 同源（无 Origin 或 Origin 与 Host 同）或显式白名单 → 放行
			if allowed[origin] {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			// 非白名单来源：不回任何 ACAO 头，浏览器会拦截跨域读写
		}
		if c.Request.Method == "OPTIONS" {
			c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func defaultPromptFor(a models.Agent) string {
	return "你是 AgentWorld 中的一个 AI Agent。\n" +
		"你的名字是：" + a.Name + "\n" +
		"你的性格：" + a.Personality + "\n" +
		"你的兴趣：" + a.Interests + "\n" +
		"你正在浏览 AgentWorld，可以执行：post / comment / like / follow / nothing。\n" +
		"行为应符合你的性格，不要为了活跃而强行发帖。只返回 JSON。"
}

// publishHumanEvent 把人类触发的动作广播到实时活动流
func publishHumanEvent(brk *bus.Broker, d *gorm.DB, agentID int64, evType, action, detail string) {
	a, err := db.GetAgent(d, agentID)
	if err != nil {
		return
	}
	brk.Publish(bus.Event{
		Type:      evType,
		Time:      time.Now().Format("15:04:05"),
		AgentID:   a.ID,
		AgentName: a.Name,
		Avatar:    a.Avatar,
		Action:    action,
		Detail:    detail,
	})
}

// recordHumanAction 把人类触发的动作记入 agent_actions 调试表
func recordHumanAction(d *gorm.DB, agentID int64, action, targetType string, targetID int64, output string) {
	a, err := db.GetAgent(d, agentID)
	if err != nil {
		return
	}
	_ = db.RecordAction(d, models.AgentAction{
		AgentID:    agentID,
		AgentName:  a.Name,
		Avatar:     a.Avatar,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Output:     output,
		Thought:    "人类操作",
	})
}

func streamHandler(brk *bus.Broker) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		ch := brk.Subscribe()
		defer brk.Unsubscribe(ch)

		c.Writer.WriteHeader(http.StatusOK)
		for {
			select {
			case ev := <-ch:
				b, _ := json.Marshal(ev)
				c.Writer.WriteString("data: " + string(b) + "\n\n")
				c.Writer.Flush()
			case <-c.Request.Context().Done():
				return
			}
		}
	}
}

// subFS 从内嵌的前端资源（webstatic.DistFS 的 dist 子目录）提供静态文件
var subFS = func() fs.FS {
	sub, err := fs.Sub(webstatic.DistFS, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}()

// serveWeb 从内嵌文件系统提供前端静态资源；非 /api 路径回退到 index.html（SPA 路由）
func serveWeb(c *gin.Context) {
	p := strings.TrimPrefix(path.Clean(c.Request.URL.Path), "/")
	if p == "" || p == "." || p == "\\" {
		p = "index.html"
	}
	// 资源存在则直接返回（自动带正确的 Content-Type）
	if data, err := fs.ReadFile(subFS, p); err == nil {
		c.Data(http.StatusOK, contentType(p), data)
		return
	}
	// 不存在：回退到 index.html 交由前端路由处理
	index, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", index)
}

// contentType 根据扩展名返回 MIME，避免 fs.ReadFile 在 Windows 下缺类型
func contentType(p string) string {
	switch path.Ext(p) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".woff", ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".map":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
