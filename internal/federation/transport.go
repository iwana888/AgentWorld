package federation

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Transport 跨实例消息传输接口。
// 当前实现为 HTTPS（HTTPClient）；后续可替换为 WebSocket / gRPC，只需实现本接口。
type Transport interface {
	// Send 把一条 RemoteMessage 投递到目标实例的 federation 端点。
	// endpoint 是目标实例的 HTTP 基准地址。
	Send(ctx context.Context, endpoint string, msg RemoteMessage) (SendResult, error)
	// FetchManifest 拉取目标实例的 Agent Manifest（分布式通讯录）。
	FetchManifest(ctx context.Context, endpoint string) (*Manifest, error)
}

// HTTPTransport 基于 HTTPS/HTTP 的 Transport 实现。
// 若配置了共享密钥 secret，发送时会对消息体做 HMAC 签名，
// 接收端校验签名，防止公网伪造/篡改消息（M12.4 安全加固）。
type HTTPTransport struct {
	client *http.Client
	secret []byte // 共享密钥；空 = 不签名（向后兼容，仅限可信内网）
}

// NewHTTPTransport 创建 HTTP Transport。
// timeout 为单次请求超时，0 用默认 10s。
// secret 为实例间共享密钥：发送消息时用于 HMAC 签名；空表示不签名。
func NewHTTPTransport(timeout time.Duration, secret string) Transport {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	var key []byte
	if secret != "" {
		key = []byte(secret)
	}
	return &HTTPTransport{client: &http.Client{Timeout: timeout}, secret: key}
}

// signHeader 对 body 计算 HMAC-SHA256 并返回 base64。secret 为空返回 ""。
func (t *HTTPTransport) signHeader(body []byte) string {
	if len(t.secret) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, t.secret)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifySignature 校验请求体的 HMAC 签名（接收端用）。secret 为空时跳过（不校验）。
func VerifySignature(secret string, body []byte, sig string) bool {
	if secret == "" || sig == "" {
		// 未配置共享密钥：跳过校验（内网可信场景）。生产建议配置。
		return secret == ""
	}
	expected := HMACSign([]byte(secret), body)
	return hmac.Equal([]byte(expected), []byte(sig))
}

// HMACSign 计算 body 的 HMAC-SHA256（base64 RawURL 编码）。供 VerifySignature 使用。
func HMACSign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// MessageSignatureHeader 消息签名请求头名。
const MessageSignatureHeader = "X-AgentWorld-Signature"

// MessagePath 远端实例接收消息的路径（挂在本实例 api router 上）。
const MessagePath = "/api/federation/messages"

// ManifestPath 远端实例暴露 Agent Manifest 的路径。
const ManifestPath = "/.well-known/agent.json"

// Send 投递一条 RemoteMessage。
func (t *HTTPTransport) Send(ctx context.Context, endpoint string, msg RemoteMessage) (SendResult, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return SendResult{}, fmt.Errorf("federation: marshal message: %w", err)
	}
	url := endpoint + MessagePath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, fmt.Errorf("federation: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// 协议版本标记：告知远端这条消息由 federation 投递。
	req.Header.Set("X-AgentWorld-Federation", "v1")
	// 若配置了共享密钥，对消息体做 HMAC 签名，供接收端校验。
	if sig := t.signHeader(body); sig != "" {
		req.Header.Set(MessageSignatureHeader, sig)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return SendResult{Delivered: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return SendResult{Delivered: false, Error: string(data)}, fmt.Errorf(
			"federation: send to %s failed: HTTP %d: %s", url, resp.StatusCode, truncate(string(data)))
	}
	// 远端可能回送落库后的消息 ID。
	var result SendResult
	result.Delivered = true
	_ = json.Unmarshal(data, &result)
	return result, nil
}

// FetchManifest 拉取远端实例的 Agent Manifest。
func (t *HTTPTransport) FetchManifest(ctx context.Context, endpoint string) (*Manifest, error) {
	url := endpoint + ManifestPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("federation: build manifest request: %w", err)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("federation: fetch manifest %s failed: HTTP %d", url, resp.StatusCode)
	}
	var m Manifest
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("federation: decode manifest: %w", err)
	}
	return &m, nil
}

// truncate 截断过长的错误信息，避免日志被撑爆。
func truncate(s string) string {
	const max = 512
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
