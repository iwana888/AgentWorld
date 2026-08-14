package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPBackend 通过 mcp-go 框架连接任意 MCP（Model Context Protocol）服务。
//
// 支持的传输：
//   - "streamable"：MCP 标准 HTTP 传输（POST 同端点 + SSE），对应 localhost:8081/mcp
//   - "sse"：旧版 HTTP+SSE 传输（GET /sse 建流 + POST /messages），对应 localhost:8081/sse
//
// MCPBackend 在构造时会：
//  1. 用 mcp-go 创建对应传输的客户端；
//  2. 发起 initialize 握手建立会话；
//  3. 拉取 tools/list，把远端工具映射为本地的 capability.Tool（惰性）。
//
// 对 Agent 而言，"调用某能力"统一走 Backend.Execute(args)，屏蔽 HTTP/MCP 差异。
type MCPBackend struct {
	URL        string
	Mode       string // "streamable"（默认） / "sse"
	Headers    map[string]string
	Timeout    time.Duration
	ClientName string
	ClientVer  string

	mu          sync.Mutex
	client      *client.Client
	initialized bool
	tools       map[string]*Tool // 本地缓存：name -> Tool（用于自省/日志，非必须）
}

// NewMCPBackend 构造 MCP 后端（Streamable HTTP 传输）。
// 尚不连接；首次 Execute 或 Refresh 时连接。
func NewMCPBackend(url string, headers map[string]string) *MCPBackend {
	return NewMCPBackendWithMode(url, headers, "streamable")
}

// NewMCPBackendWithMode 构造 MCP 后端，指定传输模式（"streamable" 或 "sse"）。
func NewMCPBackendWithMode(url string, headers map[string]string, mode string) *MCPBackend {
	if headers == nil {
		headers = map[string]string{}
	}
	if mode == "" {
		mode = "streamable"
	}
	return &MCPBackend{
		URL:        url,
		Mode:       mode,
		Headers:    headers,
		Timeout:    20 * time.Second,
		ClientName: "agentworld-capability",
		ClientVer:  "1.0.0",
		tools:      map[string]*Tool{},
	}
}

// ensure 建立并初始化 MCP 客户端（幂等，带锁）。
func (b *MCPBackend) ensure(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.initialized && b.client != nil {
		return nil
	}
	return b.connectLocked(ctx)
}

func (b *MCPBackend) connectLocked(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var c *client.Client
	var err error
	if b.Mode == "sse" {
		// 旧版 HTTP+SSE 传输（GET /sse + POST /messages）
		opts := []transport.ClientOption{
			transport.WithHeaders(b.Headers),
		}
		c, err = client.NewSSEMCPClient(b.URL, opts...)
	} else {
		// MCP 标准 Streamable HTTP 传输（POST 同端点 + SSE）
		opts := []transport.StreamableHTTPCOption{
			transport.WithHTTPHeaders(b.Headers),
			transport.WithHTTPTimeout(b.Timeout),
		}
		c, err = client.NewStreamableHttpClient(b.URL, opts...)
	}
	if err != nil {
		return fmt.Errorf("MCP 客户端创建失败: %w", err)
	}

	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2025-06-18",
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    b.ClientName,
				Version: b.ClientVer,
			},
		},
	}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return fmt.Errorf("MCP initialize 失败: %w", err)
	}
	b.client = c
	b.initialized = true
	return nil
}

// Execute 调用 MCP 工具。参数按参数名传递，依据工具参数 schema 做类型转换。
func (b *MCPBackend) Execute(args map[string]interface{}) (string, error) {
	name, _ := args["_tool"].(string)
	if name == "" {
		return "", fmt.Errorf("MCP 工具调用缺少参数 _tool（要调用的远端工具名）")
	}
	delete(args, "_tool")

	ctx, cancel := context.WithTimeout(context.Background(), b.Timeout)
	defer cancel()

	if err := b.ensure(ctx); err != nil {
		return "", err
	}

	b.mu.Lock()
	c := b.client
	b.mu.Unlock()

	// 依据工具 schema 类型归一化参数：例如 schema 声明 lockNumber 为 string，
	// 即便调用方传了数字也要转回字符串，避免远端因类型不符而判定缺参。
	norm := b.coerceArgs(name, args)

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: norm,
		},
	}
	res, err := c.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("MCP 调用 %s 失败: %w", name, err)
	}
	return formatToolResult(res), nil
}

// formatToolResult 把 mcp.CallToolResult 的 Content 数组转成人类可读文本。
func formatToolResult(res *mcp.CallToolResult) string {
	var sb strings.Builder
	if res.IsError {
		sb.WriteString("[错误] ")
	}
	for _, c := range res.Content {
		switch t := c.(type) {
		case mcp.TextContent:
			sb.WriteString(t.Text)
		case mcp.ImageContent:
			sb.WriteString("[图片]")
		case mcp.AudioContent:
			sb.WriteString("[音频]")
		case mcp.EmbeddedResource:
			sb.WriteString("[资源]")
		default:
			if b, err := json.Marshal(c); err == nil {
				sb.Write(b)
			}
		}
		sb.WriteString("\n")
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "(空结果)"
	}
	return out
}

// ListTools 拉取并返回该 MCP 服务提供的所有工具（扁平列表，含参数 schema）。
func (b *MCPBackend) ListTools() ([]Tool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), b.Timeout)
	defer cancel()

	if err := b.ensure(ctx); err != nil {
		return nil, err
	}
	b.mu.Lock()
	c := b.client
	b.mu.Unlock()

	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("MCP tools/list 失败: %w", err)
	}
	tools := make([]Tool, 0, len(res.Tools))
	b.mu.Lock()
	for _, t := range res.Tools {
		tool := Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  mcpParamsToParams(t.InputSchema),
			Backend:     b, // 共享同一后端；调用时靠 _tool 区分
			Hints:       map[string]bool{},
		}
		tools = append(tools, tool)
		b.tools[t.Name] = &tool // 缓存 schema，供 Execute 做类型强制
	}
	b.mu.Unlock()
	return tools, nil
}

// mcpParamsToParams 把 mcp 的 ToolInputSchema（JSON Schema）映射为本地 Parameter 列表。
func mcpParamsToParams(schema mcp.ToolInputSchema) []Parameter {
	var out []Parameter
	required := map[string]bool{}
	for _, r := range schema.Required {
		required[r] = true
	}
	for name, raw := range schema.Properties {
		desc := ""
		typ := "string"
		var def interface{}
		if m, ok := raw.(map[string]interface{}); ok {
			if d, ok := m["description"].(string); ok {
				desc = d
			}
			if t, ok := m["type"].(string); ok {
				typ = t
			}
			if dv, ok := m["default"]; ok {
				def = dv
			}
		}
		out = append(out, Parameter{
			Name:        name,
			Type:        typ,
			Description: desc,
			Required:    required[name],
			Default:     def,
		})
	}
	return out
}

// coerceArgs 依据本地缓存的工具参数 schema，把调用参数转换为远端期望的类型。
// 若未知工具或 schema 未缓存，则原样透传（不做无依据的类型推断）。
func (b *MCPBackend) coerceArgs(toolName string, args map[string]interface{}) map[string]interface{} {
	b.mu.Lock()
	tool := b.tools[toolName]
	b.mu.Unlock()
	typOf := map[string]string{}
	if tool != nil {
		for _, p := range tool.Parameters {
			typOf[p.Name] = p.Type
		}
	}
	out := make(map[string]interface{}, len(args))
	for k, v := range args {
		out[k] = coerceValue(v, typOf[k])
	}
	return out
}

// coerceValue 把字符串值按目标类型转换；目标类型未知/为空时保持字符串不变。
func coerceValue(v interface{}, target string) interface{} {
	s, ok := v.(string)
	if !ok || s == "" {
		return v
	}
	switch target {
	case "number", "integer":
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			if target == "integer" {
				return int64(n)
			}
			return n
		}
	case "boolean":
		if b, err := strconv.ParseBool(s); err == nil {
			return b
		}
	}
	return s // 默认保持字符串，避免破坏 lockNumber 这类 string 参数
}
