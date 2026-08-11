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
  // 地图上的"思考/发言气泡"（M6 自主性可视化）：Agent 发言时在角色位置冒泡，几秒后消失
  const speechBubble = ref<{ agent: number; name: string; text: string; x: number; y: number } | null>(null)
  let bubbleTimer: ReturnType<typeof setTimeout> | null = null
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
    // 实时状态作为回放基准（进入回放前的最新世界状态）
    if (!replayMode.value) recordBase(data)
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

  // ==================== Replay（时间旅行，M8） ====================
  // 基准：进入回放时保存的 Agent 状态起点（连接后第一次快照）。
  // 回放时从基准开始，按事件时间戳重放到目标时刻，重建那一刻的世界。
  let baseAgents: AgentRender[] = []
  let baseBodies: { agentID: number; room: string }[] = []
  let basePhase = 'loading'
  let baseRound = 0
  let baseTs = 0
  // 完整事件历史（回放用，不裁剪；实时 events 只留 200 条）
  const allEvents = ref<ObsEvent[]>([])
  const replayMode = ref(false)
  const replayTime = ref(0)
  const replayAgents = ref<AgentRender[]>([])
  const replayBodies = ref<{ agentID: number; room: string }[]>([])
  const replayPhase = ref('')

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
    allEvents.value.push(ev)   // 完整历史，供 Replay
    if (baseTs === 0 && ev.time) baseTs = ev.time
    if (replayMode.value) {
      setReplayTime(replayTime.value)  // 回放中仍收到新事件，重算当前时刻状态
      return
    }
    applyEvent(ev)
  }

  // 记录回放基准（第一次快照的 Agent 状态 + 已收事件里的最小时刻）
  function recordBase(data: GameSnapshot) {
    baseAgents = (data.agents || []).map(a => ({
      id: a.id, name: a.name, team: a.team, alive: a.alive, room: a.room, taskDone: a.taskDone,
      x: a.x, y: a.y, facing: a.facing, walking: false,
    }))
    baseBodies = data.bodies || []
    basePhase = data.phase
    baseRound = data.round
  }

  // 进入回放模式
  function enterReplay() {
    if (allEvents.value.length === 0) return
    recordBase({ ...snapshot })
    replayMode.value = true
    replayTime.value = Math.min(...allEvents.value.map(e => e.time))
    setReplayTime(replayTime.value)
  }
  function exitReplay() {
    replayMode.value = false
    // 回到实时：重建当前实时状态
    applySnapshot({ ...snapshot })
  }

  // 把世界恢复到时刻 t
  function setReplayTime(t: number) {
    replayTime.value = t
    // 从基准复制，按事件时间 <= t 逐步应用
    const agents: AgentRender[] = baseAgents.map(a => ({ ...a }))
    const bodies = baseBodies.map(b => ({ ...b }))
    let phase = basePhase
    const sorted = [...allEvents.value].sort((x, y) => x.time - y.time)
    for (const ev of sorted) {
      if (ev.time > t) break
      const d = ev.data
      switch (ev.type) {
        case 'agent.moved': {
          const m = d as unknown as MovedData
          const a = agents.find(x => x.id === m.agent)
          if (a) { a.room = m.toRoom; a.x = m.to.x; a.y = m.to.y; a.facing = m.facing }
          break
        }
        case 'agent.killed': {
          const k = d as { victim: number; room: string }
          const a = agents.find(x => x.id === k.victim)
          if (a) a.alive = false
          if (!bodies.find(b => b.agentID === k.victim)) bodies.push({ agentID: k.victim, room: k.room })
          break
        }
        case 'agent.eliminated': {
          const e2 = d as { agent: number }
          const a = agents.find(x => x.id === e2.agent)
          if (a) a.alive = false
          break
        }
        case 'meeting.started': phase = 'meeting'
        case 'game.ended': phase = 'over'
      }
    }
    replayAgents.value = agents
    replayBodies.value = bodies
    replayPhase.value = phase
  }

  // 回放时间范围（最早/最晚事件时间）
  const replayRange = (): [number, number] => {
    if (!allEvents.value.length) return [0, 0]
    const ts = allEvents.value.map(e => e.time)
    return [Math.min(...ts), Math.max(...ts)]
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
        // 地图上冒发言气泡（体现 Agent 在表达/思考）
        const r = renderAgents.value.find(x => x.id === d.agent)
        if (r) {
          speechBubble.value = { agent: r.id, name: r.name, text: d.text, x: r.x, y: r.y - 40 }
          if (bubbleTimer) clearTimeout(bubbleTimer)
          bubbleTimer = setTimeout(() => { speechBubble.value = null }, 4500)
        }
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

  onUnmounted(() => {
    if (es) es.close()
    if (bubbleTimer) clearTimeout(bubbleTimer)
  })

  loadSnapshot()
  connect()

  return {
    snapshot, renderAgents, events, connected, getAgent, fetchInspector,
    speeches, meetingReason, speechBubble,
    replayMode, replayTime, replayAgents, replayBodies, replayPhase,
    enterReplay, exitReplay, setReplayTime, replayRange,
  }
}
