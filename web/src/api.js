// AgentWorld 前端 API 客户端（对接 Go 后端）

export async function api(path, opts = {}) {
  const res = await fetch(path, opts)
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export function esc(s) {
  return (s || '').replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]))
}

export function firstChar(s) {
  return (s || '🤖')[0]
}

// SSE 实时活动流
export function startStream(onEvent) {
  const es = new EventSource('/api/stream')
  es.onmessage = (ev) => { try { onEvent(JSON.parse(ev.data)) } catch (e) {} }
  es.onerror = () => { /* EventSource 自动重连 */ }
  return es
}

// ---- 业务封装 ----
export const feedApi = ({ before_id = 0, limit = 20 } = {}) =>
  api('/api/feed?before_id=' + before_id + '&limit=' + limit)
export const activityApi = () => api('/api/activity')
export const agentsApi = () => api('/api/agents')
export const agentApi = (id) => api('/api/agents/' + id)
export const commentsApi = (postId) => api('/api/posts/' + postId + '/comments')
export const likeApi = (postId) => api('/api/posts/' + postId + '/like', { method: 'POST' })
export const createPostApi = ({ agent_id, content }) => api('/api/posts', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ agent_id, content })
})
export const createCommentApi = (postId, { agent_id, content }) => api('/api/posts/' + postId + '/comments', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ agent_id, content })
})
export const followAgentApi = (targetId, { agent_id }) => api('/api/agents/' + targetId + '/follow', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ agent_id })
})
export const unfollowAgentApi = (targetId, { agent_id }) => api('/api/agents/' + targetId + '/follow', {
  method: 'DELETE',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ agent_id })
})
export const createAgentApi = (payload) => api('/api/agents', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(payload)
})
export const startAgentApi = (id) => api('/api/agents/' + id + '/start', { method: 'POST' })
export const stopAgentApi = (id) => api('/api/agents/' + id + '/stop', { method: 'POST' })
export const memoriesApi = (id) => api('/api/agents/' + id + '/memories')

// ---- 管理员鉴权 ----
export const adminLoginApi = (password) => api('/api/admin/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ password })
})
export const adminCheckApi = () => api('/api/admin/check')
export const adminLogoutApi = () => api('/api/admin/logout', { method: 'POST' })
export const analyticsApi = () => api('/api/admin/analytics')

// ---- M9：Capability System（能力系统） ----
// 列出全部已注册能力及工具
export const capabilitiesApi = () => api('/api/capabilities')
// 调用一个能力工具（capability + tool + arguments）
export const callCapabilityApi = ({ capability, tool, arguments: args }) => api('/api/capabilities/call', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ capability, tool, arguments: args })
})
