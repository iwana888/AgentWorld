package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Backend 工具的后端执行器接口。一个工具的后端决定了"怎么调用外部世界"。
type Backend interface {
	// Execute 执行一次工具调用，返回人类可读的结果文本。
	Execute(args map[string]interface{}) (string, error)
}

// HTTPBackend 调用任意 HTTP 接口（REST / JSON）。
//
// Method 与 URL：POST / PUT / GET 等 + 完整 URL。
// 参数如何映射到请求体由 ParamMode 决定：
//   - ParamJSON：整个 args 作为 JSON body 发送（适合 REST JSON API）。
//   - ParamPath：args 中 {name} 形式的占位符替换进 URL，其余作为 JSON body。
//   - ParamQuery：args 作为 query string（适合 GET）。
//
// 响应解析（ResponseParse）：
//   - ParseText：直接返回响应体文本。
//   - ParseJSON：把响应体解析为 JSON 后返回格式化字符串（便于阅读/记忆）。
type HTTPBackend struct {
	Method    string // HTTP 方法，默认 POST
	URL       string // 请求 URL
	Headers   map[string]string // 附加请求头
	Timeout   time.Duration
	ParamMode string // json / path / query
	ResponseParse string // text / json

	client *http.Client
}

// HTTPParamMode 常量
const (
	ParamModeJSON  = "json"
	ParamModePath  = "path"
	ParamModeQuery = "query"
)

// HTTPResponseParse 常量
const (
	ResponseText = "text"
	ResponseJSON = "json"
)

// NewHTTPBackend 构造 HTTP 后端。
func NewHTTPBackend(method, url string, headers map[string]string) *HTTPBackend {
	if method == "" {
		method = http.MethodPost
	}
	return &HTTPBackend{
		Method:        method,
		URL:           url,
		Headers:       headers,
		Timeout:       15 * time.Second,
		ParamMode:     ParamModeJSON,
		ResponseParse: ResponseText,
		client:        &http.Client{},
	}
}

// Execute 实现 Backend 接口。
func (b *HTTPBackend) Execute(args map[string]interface{}) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), b.Timeout)
	defer cancel()

	method := b.Method
	if method == "" {
		method = http.MethodPost
	}
	url := b.URL

	// 1) 计算请求体 / query / path 参数
	var body io.Reader
	switch b.ParamMode {
	case ParamModeQuery:
		// 参数拼到 query
		q := urlQuery(args)
		if strings.Contains(url, "?") {
			url += "&" + q
		} else {
			url += "?" + q
		}
	case ParamModePath:
		// {name} 占位符替换，剩余作为 body
		url = replacePathParams(url, args)
		body = jsonReader(args)
	default: // json
		body = jsonReader(args)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range b.Headers {
		req.Header.Set(k, v)
	}

	hc := b.client
	if hc == nil {
		hc = &http.Client{Timeout: b.Timeout}
	} else {
		hc.Timeout = b.Timeout
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP 调用失败: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(data), 200))
	}
	return b.formatResponse(data)
}

func (b *HTTPBackend) formatResponse(data []byte) (string, error) {
	if b.ResponseParse == ResponseJSON {
		var v interface{}
		if err := json.Unmarshal(data, &v); err != nil {
			return string(data), nil // 不是合法 JSON，原样返回
		}
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return string(data), nil
		}
		return string(out), nil
	}
	return string(data), nil
}

func jsonReader(args map[string]interface{}) io.Reader {
	b, err := json.Marshal(args)
	if err != nil {
		return strings.NewReader("{}")
	}
	return bytes.NewReader(b)
}

func urlQuery(args map[string]interface{}) string {
	var parts []string
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, "&")
}

func replacePathParams(url string, args map[string]interface{}) string {
	out := url
	for k, v := range args {
		out = strings.ReplaceAll(out, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	return out
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
