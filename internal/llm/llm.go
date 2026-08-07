package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Decision Agent 决策结果（结构化 JSON）。仅保留与具体世界无关的通用字段：
// Action 是动作名（由 Module 决定取值，如社交的 post/comment、酒店的 book/checkout），
// Runtime 不解释 Action 的具体含义。目标信息通过 Target+TargetKind 表达，
// TargetKind 说明 Target 的含义（如 "post_id"/"agent_id"/"room_id"），由 Module 解释。
type Decision struct {
	Action     string `json:"action"`
	Target     int64  `json:"target"`      // 动作作用的目标 id（语义由 TargetKind 说明）
	TargetKind string `json:"target_kind"` // 目标类型：post_id / agent_id / room_id ... 由 Module 决定
	Reason     string `json:"reason"`
	Content    string `json:"content"`
	// 本次值得长期记住的内容（可选，留空则不记）
	Memory     string `json:"memory"`
	MemoryType string `json:"memory_type"` // self / about_agent / event ... 由 Module 决定
	Importance int    `json:"importance"`  // 1~5，越高越优先保留
	// M9：当 Action 以 "tool:" 开头时，ToolArgs 携带调用外部工具的 JSON 参数。
	// 由 Runtime 解释并路由到 Capability 执行，LLM 层不解析内容。
	ToolArgs map[string]interface{} `json:"tool_args,omitempty"`
}

// Client LLM 客户端（OpenAI 兼容接口）
type Client struct {
	apiKey  string
	baseURL string
	model   string
}

func New(baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = "https://api.deepseek.com/v1"
	}
	if model == "" {
		model = "deepseek-chat"
	}
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}
}

// Enabled 是否配置了真实 LLM
func (c *Client) Enabled() bool { return c.apiKey != "" }

// ModelName 当前使用的模型名
func (c *Client) ModelName() string { return c.model }

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	Model          string    `json:"model"`
	Messages       []chatMsg `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Temperature float64 `json:"temperature"`
}

type chatResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Decide 调用 LLM 生成结构化决策。未配置 key 时返回错误，由调用方走 Mock。
func (c *Client) Decide(ctx context.Context, system, user string) (*Decision, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("llm not configured")
	}
	reqBody := chatReq{
		Model: c.model,
		Messages: []chatMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.9,
	}
	reqBody.ResponseFormat.Type = "json_object"

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	hc := &http.Client{Timeout: 25 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm http %d", resp.StatusCode)
	}
	var cr chatResp
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return nil, err
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("llm empty choices")
	}
	var dec Decision
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &dec); err != nil {
		return nil, fmt.Errorf("llm parse: %w", err)
	}
	return &dec, nil
}
