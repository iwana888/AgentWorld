// Economy World 状态管理：轮询快照 + SSE 实时交易流。
import { ref, reactive, onUnmounted } from 'vue'
import type { GameSnapshot, InspectorData, ObsEvent } from '../types'

export function useGame() {
  const snapshot = reactive<GameSnapshot>({
    round: 0,
    agents: [],
    prices: {},
    openJobs: [],
    recentTx: [],
    totalWealth: 0,
    skillMarket: [],
    skillBuys: [],
  })
  const txStream = ref<ObsEvent[]>([])   // 实时交易流（SSE）
  const connected = ref(false)

  let es: EventSource | null = null

  async function loadSnapshot() {
    try {
      // 相对路径：基于当前页面 URL 解析。
      //  - 单文件部署（base './'，页面在 /）：解析为 /api/game
      //  - nginx 子路径部署（页面在 /economy/）：解析为 /economy/api/game
      const res = await fetch('api/game')
      const data = await res.json()
      applySnapshot(data)
    } catch (e) {
      console.error('load snapshot failed', e)
    }
  }

  function applySnapshot(data: GameSnapshot) {
    snapshot.round = data.round
    snapshot.agents = data.agents || []
    snapshot.prices = data.prices || {}
    snapshot.openJobs = data.openJobs || []
    snapshot.recentTx = data.recentTx || []
    snapshot.totalWealth = data.totalWealth
    snapshot.skillMarket = data.skillMarket || []
    snapshot.skillBuys = data.skillBuys || []
  }

  function connect() {
    if (es) es.close()
    // 相对路径（同 loadSnapshot 的说明，nginx 子路径下解析为 /economy/api/events/stream）
    es = new EventSource('api/events/stream')
    es.onopen = () => {
      connected.value = true
      loadSnapshot()
    }
    es.onerror = () => { connected.value = false }
    es.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as ObsEvent
        pushEvent(ev)
      } catch { /* ignore */ }
    }
  }

  function pushEvent(ev: ObsEvent) {
    txStream.value.push(ev)
    if (txStream.value.length > 200) txStream.value.splice(0, txStream.value.length - 200)
    // 某些事件后刷新快照（交易/工作会让余额/价格变化）
    if (['tx', 'job.done', 'trade.buy', 'trade.sell', 'skill.buy'].includes(ev.type)) {
      // 节流：避免频繁拉快照
      if (!snapshotUpdating) {
        snapshotUpdating = true
        setTimeout(async () => { await loadSnapshot(); snapshotUpdating = false }, 800)
      }
    }
  }
  let snapshotUpdating = false

  async function fetchInspector(id: number): Promise<InspectorData | null> {
    try {
      // 相对路径（nginx 子路径下解析为 /economy/api/agents/{id}）
      const res = await fetch(`api/agents/${id}`)
      if (!res.ok) return null
      return await res.json()
    } catch {
      return null
    }
  }

  onUnmounted(() => { if (es) es.close() })

  loadSnapshot()
  connect()

  return { snapshot, txStream, connected, fetchInspector }
}
