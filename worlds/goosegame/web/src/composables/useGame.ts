// 游戏状态管理：轮询初始快照 + SSE 实时增量。
import { ref, reactive, onUnmounted } from 'vue'
import type { AgentPublic, GameSnapshot, ObsEvent } from '../types'

export function useGame() {
  // 当前游戏状态（由快照初始化，SSE 增量更新）
  const snapshot = reactive<GameSnapshot>({
    phase: 'loading',
    round: 0,
    agents: [],
    bodies: [],
  })
  // 实时事件流（Timeline 用，最多保留 200 条）
  const events = ref<ObsEvent[]>([])
  // SSE 连接状态
  const connected = ref(false)

  let es: EventSource | null = null

  // 加载初始快照（/api/game）
  async function loadSnapshot() {
    try {
      const res = await fetch('/api/game')
      const data = await res.json()
      applySnapshot(data)
    } catch (e) {
      console.error('load snapshot failed', e)
    }
  }

  function applySnapshot(data: GameSnapshot) {
    snapshot.phase = data.phase
    snapshot.round = data.round
    snapshot.winner = data.winner
    snapshot.endedBy = data.endedBy
    snapshot.agents = data.agents || []
    snapshot.bodies = data.bodies || []
  }

  // 连接 SSE 实时事件流
  function connect() {
    if (es) es.close()
    es = new EventSource('/api/events/stream')
    es.onopen = () => {
      connected.value = true
      // 每次（重）连接成功都重新拉一次快照，确保与后端完整同步
      loadSnapshot()
    }
    es.onerror = (e) => {
      connected.value = false
      console.warn('[SSE] connection error', e)
      // EventSource 默认会自动重连，这里只标记状态
    }
    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as ObsEvent
        pushEvent(ev)
      } catch { /* ignore bad json */ }
    }
  }

  function pushEvent(ev: ObsEvent) {
    events.value.push(ev)
    if (events.value.length > 200) events.value.splice(0, events.value.length - 200)
    applyEvent(ev)
  }

  // 把事件反映到地图/状态
  function applyEvent(ev: ObsEvent) {
    switch (ev.type) {
      case 'agent.moved': {
        const d = ev.data as { agent: number; to: string }
        const a = snapshot.agents.find(x => x.id === d.agent)
        if (a) a.room = d.to
        break
      }
      case 'agent.killed': {
        const d = ev.data as { victim: number; room: string }
        const a = snapshot.agents.find(x => x.id === d.victim)
        if (a) a.alive = false
        // 尸体在地图上显示
        if (!snapshot.bodies.find(b => b.agentID === d.victim)) {
          snapshot.bodies.push({ agentID: d.victim, room: d.room })
        }
        break
      }
      case 'agent.eliminated': {
        const d = ev.data as { agent: number }
        const a = snapshot.agents.find(x => x.id === d.agent)
        if (a) a.alive = false
        break
      }
      case 'game.ended': {
        const d = ev.data as { winner: string; reason: string; endedBy: string }
        snapshot.phase = 'over'
        snapshot.winner = d.winner
        snapshot.endedBy = d.endedBy
        break
      }
      case 'meeting.started': {
        snapshot.phase = 'meeting'
        break
      }
    }
  }

  // 获取单个 Agent 的公开信息
  function getAgent(id: number): AgentPublic | undefined {
    return snapshot.agents.find(x => x.id === id)
  }

  onUnmounted(() => { if (es) es.close() })

  // 启动
  loadSnapshot()
  connect()

  return { snapshot, events, connected, getAgent }
}
