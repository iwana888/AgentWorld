// Package capability — M9 Capability System（能力系统）。
//
// 目标：让 Agent 可以连接现实（外部工具 / API / MCP 服务）。
//
//	Agent
//	  ↓
//	Capability
//	  ↓
//	Tool
//
// Capability 是"能力"的抽象，一个 Capability 可以包含多个 Tool。例如：
//   - "pms" 能力：对应酒店 PMS（Property Management System）系统，包含
//     send_room_key（发卡）/ cancel_room_key（销卡）/ read_room_key（查卡）等工具。
//   - "web_search" 能力：对应一个搜索 API，包含 search 工具。
//
// 一个 Tool 通过 Backend 决定如何执行：
//   - BackendHTTP：调用任意 HTTP 接口（REST / OpenAI 兼容）。
//   - BackendMCP：调用 MCP（Model Context Protocol）Streamable HTTP 服务。
//
// Agent 无需关心后端差异——对 Agent 而言，Capability 就是"名字 + 一组可调用的工具"。
package capability

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Parameter 工具的单个参数定义（JSON Schema 子集，供 LLM 描述与调用方校验）。
type Parameter struct {
	Name        string      `json:"name"`                   // 参数名
	Type        string      `json:"type"`                   // string / number / boolean
	Description string      `json:"description,omitempty"`  // 参数说明
	Required    bool        `json:"required,omitempty"`     // 是否必填
	Default     interface{} `json:"default,omitempty"`      // 默认值
}

// Tool 一个可被 Agent 调用的外部工具。
type Tool struct {
	Name        string            `json:"name"`                    // 工具名（能力内唯一）
	Description string            `json:"description,omitempty"`  // 工具说明（供 LLM 理解用途）
	Parameters  []Parameter       `json:"parameters,omitempty"`   // 参数定义
	Backend     Backend           `json:"-"`                      // 后端执行器
	Hints       map[string]bool   `json:"hints,omitempty"`        // 提示标注：destructive/read_only 等
}

// Capability 一组对外能力（可插拔）。对应 readme3 中 M9 的 Capability 概念。
type Capability struct {
	Name     string `json:"name"`     // 能力名，如 "pms" / "search"
	Desc     string `json:"desc"`     // 能力描述
	Tools    []*Tool `json:"tools"`   // 该能力下的工具列表
	Registry *Registry `json:"-"`     // 所属注册表（回引，可选）
}

// Execute 调用能力下的某个工具。
func (c *Capability) Execute(name string, args map[string]interface{}) (string, error) {
	for _, t := range c.Tools {
		if t.Name == name {
			return t.Execute(args)
		}
	}
	return "", fmt.Errorf("capability %s: 未找到工具 %s", c.Name, name)
}

// FindTool 在能力内查找工具。
func (c *Capability) FindTool(name string) *Tool {
	for _, t := range c.Tools {
		if t.Name == name {
			return t
		}
	}
	return nil
}

// Execute 执行一个工具，返回人类可读的结果文本。
// 对 MCP 后端：自动注入 _tool 以指明要调用的远端工具名。
func (t *Tool) Execute(args map[string]interface{}) (string, error) {
	if t.Backend == nil {
		return "", fmt.Errorf("tool %s: 未配置执行后端", t.Name)
	}
	// 校验必填参数
	for _, p := range t.Parameters {
		if p.Required {
			if _, ok := args[p.Name]; !ok {
				return "", fmt.Errorf("tool %s: 缺少必填参数 %q", t.Name, p.Name)
			}
		}
	}
	callArgs := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		callArgs[k] = v
	}
	if _, ok := t.Backend.(*MCPBackend); ok {
		callArgs["_tool"] = t.Name
	}
	return t.Backend.Execute(callArgs)
}

// Registry 能力注册表：全局持有所有已注册 Capability，供 Agent/API 查询与调用。
type Registry struct {
	mu           sync.RWMutex
	capabilities map[string]*Capability
}

// NewRegistry 创建空注册表。
func NewRegistry() *Registry {
	return &Registry{capabilities: map[string]*Capability{}}
}

// Register 注册一个能力（重名覆盖）。
func (r *Registry) Register(c *Capability) {
	if c == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c.Registry = r
	r.capabilities[c.Name] = c
}

// Get 按名字取能力。
func (r *Registry) Get(name string) *Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities[name]
}

// List 返回全部能力（复制切片，安全遍历）。
func (r *Registry) List() []*Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Capability, 0, len(r.capabilities))
	for _, c := range r.capabilities {
		out = append(out, c)
	}
	return out
}

// Count 已注册能力数。
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.capabilities)
}

// Tools 返回所有能力下所有工具的扁平列表（供 LLM tool calling 描述）。
func (r *Registry) Tools() []*Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Tool
	for _, c := range r.capabilities {
		out = append(out, c.Tools...)
	}
	return out
}

// JSON 返回注册表的 JSON 摘要（供 API 展示）。
func (r *Registry) JSON() string {
	type toolView struct {
		Name        string            `json:"name"`
		Description string            `json:"description,omitempty"`
		Parameters  []Parameter       `json:"parameters,omitempty"`
		Hints       map[string]bool   `json:"hints,omitempty"`
	}
	type capView struct {
		Name  string      `json:"name"`
		Desc  string      `json:"desc,omitempty"`
		Tools []toolView  `json:"tools"`
	}
	caps := r.List()
	views := make([]capView, 0, len(caps))
	for _, c := range caps {
		v := capView{Name: c.Name, Desc: c.Desc}
		for _, t := range c.Tools {
			v.Tools = append(v.Tools, toolView{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
				Hints:       t.Hints,
			})
		}
		views = append(views, v)
	}
	b, _ := json.MarshalIndent(views, "", "  ")
	return string(b)
}
