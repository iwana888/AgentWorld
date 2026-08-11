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
        <div class="left-top">
          <WorldView :snapshot="displaySnapshot" :agents="displayAgents" :speech-bubble="speechBubble" @select="onSelect" />
          <MeetingOverlay
            :show="snapshot.phase === 'meeting'"
            :reason="meetingReason"
            :agents="snapshot.agents"
            :speeches="speeches" />
        </div>
        <ReplayTimeline
          :replay-mode="replayMode"
          :replay-time="replayTime"
          :replay-phase="replayPhase"
          :range="replayRange()"
          @enter="enterReplay" @exit="exitReplay" @seek="setReplayTime" />
      </div>
      <div class="right">
        <AgentPanel
          :agent="selectedAgent"
          :agents="snapshot.agents"
          :load-inspector="fetchInspector" />
        <div class="timeline-box">
          <Timeline :events="events" :connected="connected" @explain="onExplain" />
        </div>
      </div>
    </div>

    <!-- "为什么"弹窗：点开 Timeline 事件，展示该 Agent 这次决策的依据 -->
    <el-dialog v-model="whyVisible" :title="whyTitle" width="440px" class="why-dialog">
      <div v-if="whyInspector" class="why-dialog-body">
        <div class="why-hero">
          <span class="why-emoji">🧑</span>
          <div>
            <div class="why-name">{{ whyInspector.name }}</div>
            <div class="why-role">{{ whyInspector.goal }}</div>
          </div>
        </div>
        <div class="why-lines">
          <div v-for="(line, i) in whyParsed" :key="i" class="why-line" :class="{ act: line.key === '因此' }">
            <span class="why-key">{{ line.key }}</span>
            <span class="why-val">{{ line.val }}</span>
          </div>
          <div v-if="!whyParsed.length" class="why-empty">暂无决策记录</div>
        </div>
      </div>
    </el-dialog>

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
import type { InspectorData } from './types'
import ReplayTimeline from './components/ReplayTimeline.vue'

const {
  snapshot, renderAgents, events, connected, getAgent, fetchInspector,
  speeches, meetingReason, speechBubble,
  replayMode, replayTime, replayAgents, replayBodies, replayPhase,
  enterReplay, exitReplay, setReplayTime, replayRange,
} = useGame()

const selected = ref<number | null>(null)

function onSelect(id: number) {
  // 点已选中的角色则取消选择
  selected.value = selected.value === id ? null : id
}

const selectedAgent = computed(() => selected.value ? getAgent(selected.value) ?? null : null)

// 回放模式下用回放状态渲染地图（时间旅行：世界恢复到指定时刻）
const displaySnapshot = computed(() => {
  if (replayMode.value) {
    return { ...snapshot, phase: replayPhase.value, bodies: replayBodies.value, agents: snapshot.agents }
  }
  return snapshot
})
const displayAgents = computed(() => (replayMode.value ? replayAgents.value : renderAgents.value))

// "为什么"弹窗状态
const whyVisible = ref(false)
const whyInspector = ref<InspectorData | null>(null)
const whyTitle = ref('')
async function onExplain(agentId: number) {
  const ins = await fetchInspector(agentId)
  if (!ins) return
  whyInspector.value = ins
  whyTitle.value = `${ins.name} · 为什么这么做`
  whyVisible.value = true
}
const whyParsed = computed(() => {
  const why = whyInspector.value?.lastWhy
  if (!why) return []
  return why.split('\n').filter(Boolean).map(line => {
    const idx = line.indexOf('：')
    const key = idx > 0 ? line.slice(0, idx) : ''
    const val = idx > 0 ? line.slice(idx + 1) : line
    return { key, val }
  })
})

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
.left { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 10px; }
.left-top { flex: 1; min-height: 0; position: relative; }
.right { width: 340px; display: flex; flex-direction: column; gap: 12px; }
.timeline-box { flex: 1; min-height: 0; }

.status-bar { display: flex; align-items: center; gap: 18px; padding: 8px 14px; background: #10162a;
  border: 1px solid #232f4a; border-radius: 10px; color: #c6d0e8; font-size: 13px; }
.stat { display: flex; align-items: center; gap: 6px; }
.stat-emoji { font-size: 16px; }
.stat-num { color: #7cc3ff; font-weight: 800; font-size: 16px; }
.stat.task { flex: 1; justify-content: flex-end; }
.task-bar { flex: 1; max-width: 260px; }

.why-dialog :deep(.el-dialog) { background: #131b2e; border: 1px solid #2f3d5e; color: #e2e9ff; }
.why-dialog :deep(.el-dialog__title) { color: #e2e9ff; }
.why-dialog-body { display: flex; flex-direction: column; gap: 14px; }
.why-hero { display: flex; align-items: center; gap: 12px; padding-bottom: 12px; border-bottom: 1px solid #2a3550; }
.why-emoji { font-size: 34px; }
.why-name { font-size: 16px; font-weight: 800; color: #e2e9ff; }
.why-role { font-size: 12px; color: #7cc3ff; margin-top: 2px; }
.why-lines { display: flex; flex-direction: column; gap: 7px; }
.why-line { font-size: 13px; display: flex; gap: 8px; line-height: 1.5; }
.why-key { color: #7cc3ff; font-weight: 700; flex-shrink: 0; }
.why-val { color: #c6d0e8; }
.why-line.act { background: #243050; border-radius: 8px; padding: 6px 10px; margin-top: 2px; }
.why-line.act .why-key, .why-line.act .why-val { color: #ffd166; font-weight: 700; }
.why-empty { color: #5a6278; font-size: 12px; }
</style>
