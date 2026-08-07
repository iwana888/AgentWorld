package federation

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"agentworld/internal/a2a"
	"agentworld/internal/logx"
	"agentworld/sdk"
)

// Endpoint 本实例的 Federation 服务端：暴露 Agent Manifest 并接收远端消息。
// 挂在本实例的 HTTP router 上（/api/federation/* 与 /.well-known/agent.json）。
//
// 职责：
//   - 接收远端投递的 RemoteMessage，转为本地 sdk.Message 并写入本地 Bus（A2A Inbox）。
//   - 暴露本实例 Agent Manifest（分布式通讯录）。
type Endpoint struct {
	// A2A 本地消息总线：远端消息最终落进本地 Agent 的 Inbox。
	Bus *a2a.Bus
	// worldName 本世界（实例）名，Manifest 用。
	worldName string
	// endpoint 本实例对外地址（Manifest 用，供远端回发）。
	endpoint string
	// secret 共享密钥：校验远端消息的 HMAC 签名；空 = 不校验（内网可信场景）。
	secret string
}

// NewEndpoint 创建 Federation 服务端。
// worldName 是本世界名，endpoint 是本实例的 HTTP 基准地址。
// secret 是实例间共享密钥：校验远端消息签名，防止公网伪造；空表示不校验。
func NewEndpoint(bus *a2a.Bus, worldName, endpoint, secret string) *Endpoint {
	return &Endpoint{Bus: bus, worldName: worldName, endpoint: endpoint, secret: secret}
}

// HandleManifest 暴露 Agent Manifest（GET /.well-known/agent.json）。
// 由 api router 桥接调用。
func (e *Endpoint) HandleManifest(w http.ResponseWriter, r *http.Request) {
	m := e.BuildManifest()
	writeJSON(w, http.StatusOK, m)
}

// HandleMessage 接收远端投递的消息（POST /api/federation/messages）。
// 由 api router 桥接调用。
func (e *Endpoint) HandleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ginH{"error": "method not allowed"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ginH{"error": "read body: " + err.Error()})
		return
	}

	// 安全加固：若配置了共享密钥，校验远端消息的 HMAC 签名，
	// 防止公网伪造发送方/篡改消息内容。
	if e.secret != "" {
		sig := r.Header.Get(MessageSignatureHeader)
		if !VerifySignature(e.secret, data, sig) {
			writeJSON(w, http.StatusUnauthorized, ginH{"error": "invalid federation signature"})
			return
		}
	}

	var rm RemoteMessage
	if err := json.Unmarshal(data, &rm); err != nil {
		writeJSON(w, http.StatusBadRequest, ginH{"error": "invalid json: " + err.Error()})
		return
	}
	if rm.Intent == "" {
		writeJSON(w, http.StatusBadRequest, ginH{"error": "intent required"})
		return
	}
	if rm.From.World == "" || rm.From.Agent == 0 {
		writeJSON(w, http.StatusBadRequest, ginH{"error": "from.world and from.agent required"})
		return
	}
	// 目标 Agent 必须存在于本实例，防止向任意/不存在的 agent 灌消息。
	if rm.To != 0 {
		if _, err := e.Bus.AgentName(rm.To); err != nil {
			writeJSON(w, http.StatusBadRequest, ginH{"error": "unknown to agent"})
			return
		}
	}

	// 远端消息 → 本地 sdk.Message → 本地 Bus 落库进 Inbox。
	// From 用负值区间编码"远端来源"，避免与本地 AgentID 冲突，且可回信（见 ToRemoteFrom）。
	local := sdk.Message{
		From:          remoteFromToLocal(rm.From),
		To:            rm.To,
		Intent:        rm.Intent,
		Payload:       rm.Payload,
		Status:        sdk.MsgStatusPending,
		ReplyTo:       rm.ReplyTo,
		CorrelationID: rm.CorrelationID,
		CreatedAt:     time.Now(),
	}
	if err := e.Bus.Send(local); err != nil {
		writeJSON(w, http.StatusInternalServerError, ginH{"error": "route: " + err.Error()})
		return
	}
	logx.Infof("federation: 收到远端消息 intent=%s from=%s/%d to=%d",
		rm.Intent, rm.From.World, rm.From.Agent, rm.To)
	writeJSON(w, http.StatusOK, ginH{"delivered": true})
}

// BuildManifest 构造本实例的 Agent Manifest（分布式通讯录）。
// 从本地 Bus 的 Registry 拉取全部能力声明，聚合为 Manifest。
func (e *Endpoint) BuildManifest() *Manifest {
	m := &Manifest{
		Name:     e.worldName,
		Runtime:  "agentworld",
		Endpoint: e.endpoint,
		Agents:   []ManifestAgent{},
	}
	if e.Bus == nil {
		return m
	}
	// 遍历本地通讯录，按 Agent 聚合其 skills。
	// registry.All() 返回全部能力声明（见 internal/a2a/registry.go）。
	byAgent := map[int64]*ManifestAgent{}
	order := []int64{}
	for _, c := range e.Bus.Registry().All() {
		ma, ok := byAgent[c.AgentID]
		if !ok {
			ma = &ManifestAgent{ID: c.AgentID, Skills: []string{}}
			byAgent[c.AgentID] = ma
			order = append(order, c.AgentID)
		}
		ma.Skills = append(ma.Skills, c.Skill)
	}
	for _, id := range order {
		ma := byAgent[id]
		if sa, err := e.Bus.AgentName(id); err == nil {
			ma.Name = sa
		}
		m.Agents = append(m.Agents, *ma)
	}
	return m
}

// ---- 寻址编码 ----
//
// 远端 Agent 在本地没有 AgentID，但消息表需要 from_agent。用负值区间编码：
//   - local = -(world 哈希 + agentID) 的一种稳定映射，保证同一远端 agent 恒定。
//   - 回信时用 ToRemoteFrom 从本地编码还原 remote addr。
//
// 负值区间避开本地正整数 AgentID，避免冲突。

// remoteFromToLocal 把远端 (world, agent) 编码为本地负值 from_agent。
// 用 FNV 哈希 world 保证稳定性与低碰撞。
func remoteFromToLocal(f FromRef) int64 {
	h := fnv64(f.World)
	return -(h ^ int64(f.Agent))
}

// fnv64 简化的 FNV-1a 64 位哈希，用于把 world 名映射为整数。
func fnv64(s string) int64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return int64(h)
}

// ginH 轻量 JSON map（避免为 endpoint 引入 gin 依赖，同时兼容 gin.H 语义）。
type ginH map[string]interface{}

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
