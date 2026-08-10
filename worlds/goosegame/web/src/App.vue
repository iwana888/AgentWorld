<template>
  <div class="app">
    <header class="app-header">
      <div class="title">
        <span class="logo">🦢</span>
        <span>AgentWorld · AI 社会观察台</span>
        <span class="sub">Goose Game</span>
      </div>
      <div class="meta">
        <el-tag size="small" effect="dark">第 {{ snapshot.round }} 回合</el-tag>
        <el-tag size="small" :type="phaseTag" effect="dark">{{ phaseText }}</el-tag>
        <el-tag v-if="snapshot.phase === 'over'" size="small" type="success" effect="dark">
          {{ snapshot.winner }} 胜
        </el-tag>
      </div>
    </header>

    <div class="app-body">
      <div class="left">
        <WorldView :snapshot="snapshot" @select="selected = $event" />
      </div>
      <div class="right">
        <AgentPanel :agent="selectedAgent" />
        <div class="timeline-box">
          <Timeline :events="events" :connected="connected" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import WorldView from './components/WorldView.vue'
import AgentPanel from './components/AgentPanel.vue'
import Timeline from './components/Timeline.vue'
import { useGame } from './composables/useGame'

const { snapshot, events, connected, getAgent } = useGame()
const selected = ref<number | null>(null)

const selectedAgent = computed(() => selected.value ? getAgent(selected.value) ?? null : null)

const phaseText = computed(() => {
  const p = snapshot.phase
  return p === 'meeting' ? '会议中' : p === 'over' ? '已结束' : '行动阶段'
})
const phaseTag = computed(() => {
  return snapshot.phase === 'meeting' ? 'warning' : snapshot.phase === 'over' ? 'info' : 'primary'
})
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
.meta { display: flex; gap: 6px; }
.app-body { flex: 1; display: flex; gap: 12px; min-height: 0; }
.left { flex: 1; min-width: 0; }
.right { width: 340px; display: flex; flex-direction: column; gap: 12px; }
.timeline-box { flex: 1; min-height: 0; }
</style>
