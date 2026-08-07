package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

	"agentworld/internal/config"

	"github.com/gin-gonic/gin"
)

// tokenPayload 存放在 cookie 中的认证数据
type tokenPayload struct {
	User  string `json:"u"`
	ExpAt int64  `json:"e"`
}

// sign 对 payload 做 HMAC 签名并返回完整 token (base64)
func signToken(p tokenPayload) (string, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(config.C.JWTSecret))
	mac.Write(data)
	sig := mac.Sum(nil)
	token := append(data, sig...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

// verifyToken 验证 token，成功返回 payload
func verifyToken(raw string) (*tokenPayload, error) {
	token, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	// token = payload(JSON) + signature(32 bytes)
	if len(token) < 33 {
		return nil, errTokenInvalid
	}
	data := token[:len(token)-32]
	sig := token[len(token)-32:]

	mac := hmac.New(sha256.New, []byte(config.C.JWTSecret))
	mac.Write(data)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return nil, errTokenInvalid
	}

	var p tokenPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.ExpAt < time.Now().Unix() {
		return nil, errTokenExpired
	}
	return &p, nil
}

var (
	errTokenInvalid = &authError{"token invalid"}
	errTokenExpired = &authError{"token expired"}
)

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

// AuthMiddleware Gin 中间件：检查登录 cookie（admin 或 human 任意一种即可）。
// 本地实验/单管理员场景下，admin 与人类身份共用写接口鉴权；
// 人类身份登录（aw_human_token）同样视为已登录用户，可调用写接口。
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 依次尝试 admin 与 human 两种 token
		for _, name := range []string{"aw_admin_token", "aw_human_token"} {
			token, err := c.Cookie(name)
			if err == nil && token != "" {
				if _, verr := verifyToken(token); verr == nil {
					c.Next()
					return
				}
			}
		}
		c.AbortWithStatusJSON(401, gin.H{"error": "未登录或登录已过期"})
	}
}

// handler: POST /api/admin/login
func adminLogin(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if req.Password != config.C.AdminPassword {
		c.JSON(401, gin.H{"error": "密码错误"})
		return
	}

	p := tokenPayload{
		User:  "admin",
		ExpAt: time.Now().Add(24 * time.Hour).Unix(),
	}
	token, err := signToken(p)
	if err != nil {
		c.JSON(500, gin.H{"error": "签发失败"})
		return
	}

	// 仅在 dev 环境使用 Secure:false，生产环境需 HTTPS
	c.SetCookie("aw_admin_token", token, 86400, "/", "", false, true)
	c.JSON(200, gin.H{"ok": true})
}

// handler: POST /api/admin/logout
func adminLogout(c *gin.Context) {
	c.SetCookie("aw_admin_token", "", -1, "/", "", false, true)
	c.JSON(200, gin.H{"ok": true})
}

// handler: GET /api/admin/check
func adminCheck(c *gin.Context) {
	token, err := c.Cookie("aw_admin_token")
	if err != nil {
		c.JSON(401, gin.H{"ok": false, "error": "未登录"})
		return
	}
	_, err = verifyToken(token)
	if err != nil {
		c.JSON(401, gin.H{"ok": false, "error": "登录已过期"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
