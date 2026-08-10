// 游戏状态管理：轮询初始快照 + SSE 实时增量 + Agent 2D 坐标动画。
import { ref, reactive, onUnmounted } from 'vue'
import type { AgentPublic, GameSnapshot, InspectorData, MovedData, MeetingData, ObsEvent, SpokeData } from '../types'

// 一个 Agent 的渲染状态（含 2D 坐标与朝向）
export interface AgentRender {
  id: number
  name: string
  team: string
  alive: boolean
  room: string
  taskDone: number
  x: number          // 目标坐标（后端真实位置）
  y: number
  facing: number     // 朝向（弧度）
  walking: boolean   // 是否正在移动（走路动画）
}

export function useGame() {
  const snapshot = reactive<GameSnapshot>({
    phase: 'loading',
    round: 0,
    agents: [],
    bodies: [],
  })
  const renderAgents = ref<AgentRender[]>([])
  const events = ref<ObsEvent[]>([])
  const speeches = ref<{ name: string; team: string; text: string; time: number }[]>([])
  const meetingReason = ref('')
  const connected = ref(false)

  let es: EventSource | null = null

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
    // 渲染列表：坐标直接来自后端（M5.1 真实空间位置）
    const existing = new Map(renderAgents.value.map(a => [a.id, a]))
    renderAgents.value = snapshot.agents.map(a => {
      const prev = existing.get(a.id)
      return {
        id: a.id, name: a.name, team: a.team, alive: a.alive,
        room: a.room, taskDone: a.taskDone,
        x: a.x, y: a.y, facing: a.facing,
        walking: prev?.walking ?? false,
      }
    })
  }

  function connect() {
    if (es) es.close()
    es = new EventSource('/api/events/stream')
    es.onopen = () => {
      connected.value = true
      loadSnapshot()
    }
    es.onerror = () => {
      connected.value = false
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

  function applyEvent(ev: ObsEvent) {
    switch (ev.type) {
      case 'agent.moved': {
        const d = ev.data as unknown as MovedData
        const a = snapshot.agents.find(x => x.id === d.agent)
        if (a) {
          a.room = d.toRoom
          a.x = d.to.x
          a.y = d.to.y
          a.facing = d.facing
        }
        const r = renderAgents.value.find(x => x.id === d.agent)
        if (r) {
          r.room = d.toRoom
          r.x = d.to.x
          r.y = d.to.y
          r.facing = d.facing
          // 走路动画：过渡期间标记 walking，结束后清除
          r.walking = true
          setTimeout(() => { r.walking = false }, 850)
        }
        break
      }
      case 'agent.killed': {
        const d = ev.data as { victim: number; room: string }
        const a = snapshot.agents.find(x => x.id === d.victim)
        if (a) a.alive = false
        const r = renderAgents.value.find(x => x.id === d.victim)
        if (r) r.alive = false
        if (!snapshot.bodies.find(b => b.agentID === d.victim)) {
          snapshot.bodies.push({ agentID: d.victim, room: d.room })
        }
        break
      }
      case 'agent.eliminated': {
        const d = ev.data as { agent: number }
        const a = snapshot.agents.find(x => x.id === d.agent)
        if (a) a.alive = false
        const r = renderAgents.value.find(x => x.id === d.agent)
        if (r) r.alive = false
        break
      }
      case 'meeting.started': {
        const d = ev.data as MeetingData
        snapshot.phase = 'meeting'
        meetingReason.value = d.reason || '发现尸体'
        speeches.value = []
        break
      }
      case 'agent.spoke': {
        const d = ev.data as SpokeData
        const a = snapshot.agents.find(x => x.id === d.agent)
        speeches.value.push({ name: d.name, team: a?.team || 'Goose', text: d.text, time: ev.time })
        break
      }
      case 'game.ended': {
        const d = ev.data as { winner: string; reason: string; endedBy: string }
        snapshot.phase = 'over'
        snapshot.winner = d.winner
        snapshot.endedBy = d.endedBy
        break
      }
    }
  }

  async function fetchInspector(id: number): Promise<InspectorData | null> {
    try {
      const res = await fetch(`/api/agents/${id}`)
      if (!res.ok) return null
      return await res.json()
    } catch {
      return null
    }
  }

  function getAgent(id: number): AgentPublic | undefined {
    return snapshot.agents.find(x => x.id === id)
  }

  onUnmounted(() => { if (es) es.close() })

  loadSnapshot()
  connect()

  return {
    snapshot, renderAgents, events, connected, getAgent, fetchInspector,
    speeches, meetingReason,
  }
}
