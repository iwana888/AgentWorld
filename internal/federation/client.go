package federation

import (
	"context"
	"sync"
	"time"
)

// Client 本实例的 Federation 客户端：管理已发现的远端实例，并发送跨实例消息。
// 它持有 Transport 用于实际网络传输，并维护一份"远端通讯录缓存"（endpoint → Manifest）。
type Client struct {
	transport Transport
	worldName string // 本世界（实例）名，作为 RemoteMessage.From.World。

	mu     sync.RWMutex
	remote map[string]*Manifest // endpoint → 已发现的远端 Manifest 缓存
}

// NewClient 创建 Federation 客户端。
// worldName 是本世界名（远端回信时作为 From.World）。
func NewClient(transport Transport, worldName string) *Client {
	if transport == nil {
		transport = NewHTTPTransport(10*time.Second, "")
	}
	return &Client{
		transport: transport,
		worldName: worldName,
		remote:    map[string]*Manifest{},
	}
}

// SendRemote 发送一条跨实例消息到远端 Agent。
// addr.Endpoint 为目标实例地址；addr.World / addr.AgentID 为目标 Agent。
// 返回远端确认结果。失败时（delivered=false）返回错误。
func (c *Client) SendRemote(ctx context.Context, addr RemoteAddr, msg RemoteMessage) (SendResult, error) {
	if addr.Endpoint == "" {
		return SendResult{}, errEmptyEndpoint
	}
	if msg.From.World == "" {
		msg.From.World = c.worldName
	}
	res, err := c.transport.Send(ctx, addr.Endpoint, msg)
	if err != nil {
		return res, err
	}
	// 发送成功后，顺手刷新远端通讯录缓存（能力可能变化）。
	c.refresh(ctx, addr.Endpoint)
	return res, nil
}

// DiscoverRemote 拉取并缓存远端实例的 Agent Manifest（分布式通讯录）。
// 之后可通过 RemoteAgents(skill) 在远端实例中按能力找 Agent。
func (c *Client) DiscoverRemote(ctx context.Context, endpoint string) (*Manifest, error) {
	m, err := c.transport.FetchManifest(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.remote[endpoint] = m
	c.mu.Unlock()
	return m, nil
}

// RemoteAgents 在已知的远端通讯录中，按 skill 前缀查找候选 RemoteAddr。
// 返回所有"能处理该意图"的远端 Agent 的可寻址信息。
func (c *Client) RemoteAgents(skill string) []RemoteAddr {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []RemoteAddr
	for endpoint, m := range c.remote {
		for _, ag := range m.Agents {
			if hasSkill(ag.Skills, skill) {
				out = append(out, RemoteAddr{
					Endpoint: endpoint,
					World:    m.Name,
					AgentID:  ag.ID,
				})
			}
		}
	}
	return out
}

// Remotes 返回已发现的所有远端实例（endpoint 列表）。
func (c *Client) Remotes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.remote))
	for ep := range c.remote {
		out = append(out, ep)
	}
	return out
}

// refresh 刷新某个远端实例的通讯录缓存（忽略错误，仅尽力而为）。
func (c *Client) refresh(ctx context.Context, endpoint string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if m, err := c.transport.FetchManifest(ctx, endpoint); err == nil {
		c.mu.Lock()
		c.remote[endpoint] = m
		c.mu.Unlock()
	}
}

// hasSkill 判断 agent 是否提供匹配 skill 的能力（精确或前缀，点分版本匹配）。
func hasSkill(skills []string, skill string) bool {
	for _, s := range skills {
		if s == skill || (len(s) > len(skill) && s[:len(skill)] == skill && s[len(skill)] == '.') {
			return true
		}
	}
	return false
}

var errEmptyEndpoint = &ErrEmptyEndpoint{}

// ErrEmptyEndpoint 目标端点为空（无法寻址远端）。
type ErrEmptyEndpoint struct{}

func (e *ErrEmptyEndpoint) Error() string { return "federation: 远端 endpoint 为空" }
