<template>
  <div class="app">
    <header class="app-header">
      <div class="title">
        <span class="logo">🦢</span>
        <span>AgentWorld · 鸭鹅杀</span>
        <span class="sub">M5 Observatory</span>
      </div>
      <div class="meta">
        <el-tag size="small" effect="dark">第 {{ snapshot.round }} 回合</el-tag>
        <el-tag size="small" :type="phaseTag" effect="dark">{{ phaseText }}</el-tag>
        <el-tag v-if="snapshot.phase === 'over'" size="small" type="success" effect="dark">
          {{ emoji(snapshot.winner) }} {{ snapshot.winner }} 胜
        </el-tag>
        <el-tag :type="connected ? 'success' : 'danger'" size="small" effect="plain">
          {{ connected ? '● 实时' : '○ 断开' }}
        </el-tag>
      </div>
    </header>

    <div class="app-body">
      <div class="left">
        <WorldView :snapshot="snapshot" :agents="renderAgents" @select="onSelect" />
        <MeetingOverlay
          :show="snapshot.phase === 'meeting'"
          :reason="meetingReason"
          :agents="snapshot.agents"
          :speeches="speeches" />
      </div>
      <div class="right">
        <AgentPanel
          :agent="selectedAgent"
          :agents="snapshot.agents"
          :load-inspector="fetchInspector" />
        <div class="timeline-box">
          <Timeline :events="events" :connected="connected" />
        </div>
      </div>
    </div>

    <!-- 底部状态栏（不暴露隐藏身份：观众看的是角色，不是身份） -->
    <footer class="status-bar">
      <div class="stat">
        <span class="stat-emoji">👥</span> 存活
        <span class="stat-num">{{ aliveCount }}</span>
      </div>
      <div class="stat">
        <span class="stat-emoji">🗺️</span> 房间
        <span class="stat-num">{{ occupiedRooms }}</span>
      </div>
      <div class="stat task">
        <span>🔧 任务进度</span>
        <el-progress :percentage="taskPct" :stroke-width="10" class="task-bar" />
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import WorldView from './components/WorldView.vue'
import MeetingOverlay from './components/MeetingOverlay.vue'
import AgentPanel from './components/AgentPanel.vue'
import Timeline from './components/Timeline.vue'
import { useGame } from './composables/useGame'
import { teamEmoji } from './types'

const {
  snapshot, renderAgents, events, connected, getAgent, fetchInspector,
  speeches, meetingReason,
} = useGame()

const selected = ref<number | null>(null)

function onSelect(id: number) {
  // 点已选中的角色则取消选择
  selected.value = selected.value === id ? null : id
}

const selectedAgent = computed(() => selected.value ? getAgent(selected.value) ?? null : null)

// 底部状态栏：存活总数 / 有人的房间数 / 任务进度（不暴露身份分组）
const aliveCount = computed(() => snapshot.agents.filter(a => a.alive).length)
const occupiedRooms = computed(() => {
  const rooms = new Set(snapshot.agents.filter(a => a.alive).map(a => a.room))
  return rooms.size
})
const taskPct = computed(() => {
  const gooses = snapshot.agents.filter(a => a.team === 'Goose')
  if (!gooses.length) return 0
  const max = gooses.length * 15
  const done = gooses.reduce((s, a) => s + a.taskDone, 0)
  return Math.min(100, Math.round((done / max) * 100))
})

const phaseText = computed(() => {
  const p = snapshot.phase
  return p === 'meeting' ? '🚨 会议中' : p === 'over' ? '已结束' : '行动阶段'
})
const phaseTag = computed(() => {
  return snapshot.phase === 'meeting' ? 'warning' : snapshot.phase === 'over' ? 'info' : 'primary'
})
function emoji(t?: string) {
  return teamEmoji(t || 'Goose')
}
</script>

<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { background: #0b0f1a; font-family: -apple-system, "Segoe UI", "PingFang SC", sans-serif; }
#app { height: 100vh; }
.app { display: flex; flex-direction: column; height: 100vh; padding: 12px; gap: 12px; }
.app-header { display: flex; align-items: center; justify-content: space-between; color: #e2e9ff; }
.title { font-size: 18px; font-weight: 800; display: flex; align-items: center; gap: 8px; }
.logo { font-size: 22px; }
.sub { font-size: 12px; color: #7a86a6; font-weight: 400; }
.meta { display: flex; gap: 6px; align-items: center; }
.app-body { flex: 1; display: flex; gap: 12px; min-height: 0; }
.left { flex: 1; min-width: 0; position: relative; }
.right { width: 340px; display: flex; flex-direction: column; gap: 12px; }
.timeline-box { flex: 1; min-height: 0; }

.status-bar { display: flex; align-items: center; gap: 18px; padding: 8px 14px; background: #10162a;
  border: 1px solid #232f4a; border-radius: 10px; color: #c6d0e8; font-size: 13px; }
.stat { display: flex; align-items: center; gap: 6px; }
.stat-emoji { font-size: 16px; }
.stat-num { color: #7cc3ff; font-weight: 800; font-size: 16px; }
.stat.task { flex: 1; justify-content: flex-end; }
.task-bar { flex: 1; max-width: 260px; }
</style>
