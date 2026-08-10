<template>
  <div class="timeline">
    <div class="timeline-header">
      <span>实时事件</span>
      <el-tag :type="connected ? 'success' : 'danger'" size="small">
        {{ connected ? '已连接' : '断开' }}
      </el-tag>
    </div>
    <el-scrollbar class="timeline-scroll" ref="scrollRef">
      <div v-for="(ev, i) in events" :key="i" class="timeline-item">
        <span class="t-time">{{ timeStr(ev.time) }}</span>
        <span class="t-type" :class="typeClass(ev.type)">{{ typeLabel(ev.type) }}</span>
        <span class="t-text">{{ textFor(ev) }}</span>
      </div>
      <div v-if="events.length === 0" class="empty">等待事件…</div>
    </el-scrollbar>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import type { ObsEvent } from '../types'

const props = defineProps<{ events: ObsEvent[]; connected: boolean }>()
const scrollRef = ref()

// 自动滚动到底部
watch(() => props.events.length, async () => {
  await nextTick()
  const el = scrollRef.value
  if (el) el.setScrollTop(el.wrapRef?.scrollHeight ?? 0)
})

function timeStr(ms: number) {
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function typeLabel(t: string) {
  const map: Record<string, string> = {
    'agent.moved': '移动', 'task.completed': '任务', 'agent.killed': '击杀',
    'body.found': '发现尸体', 'meeting.started': '会议', 'agent.spoke': '发言',
    'vote.cast': '投票', 'agent.eliminated': '淘汰', 'game.ended': '结束',
    'world.event': '事件',
  }
  return map[t] || t
}
function typeClass(t: string) {
  const map: Record<string, string> = {
    'agent.killed': 'kill', 'meeting.started': 'meeting', 'vote.cast': 'vote',
    'agent.eliminated': 'kill', 'game.ended': 'end', 'agent.spoke': 'speak',
  }
  return map[t] || ''
}
function textFor(ev: ObsEvent) {
  const d = ev.data
  switch (ev.type) {
    case 'agent.moved': return `${d.name} → ${d.to}`
    case 'task.completed': return `${d.name} 完成任务(${d.progress})`
    case 'agent.killed': return `${d.name} 在 ${d.room} 被发现死亡`
    case 'meeting.started': return `会议开始：${d.reason}`
    case 'agent.spoke': return `${d.name}：${d.text}`
    case 'vote.cast': return `${d.name} 投票`
    case 'agent.eliminated': return `${d.name}（${d.team}）被淘汰`
    case 'game.ended': return `游戏结束 · ${d.winner} 胜：${d.reason}`
    case 'world.event': return d.text || ''
    default: return JSON.stringify(d) || ''
  }
}
</script>

<style scoped>
.timeline { display: flex; flex-direction: column; height: 100%; }
.timeline-header { display: flex; align-items: center; justify-content: space-between; color: #e2e9ff; font-weight: 700; padding-bottom: 8px; border-bottom: 1px solid #2a3550; }
.timeline-scroll { flex: 1; margin-top: 8px; }
.timeline-item { font-size: 12px; padding: 3px 0; color: #aeb9d6; display: flex; gap: 6px; align-items: baseline; }
.t-time { color: #5f6c8f; flex-shrink: 0; }
.t-type { flex-shrink: 0; }
.t-type.kill { color: #ff6b6b; }
.t-type.meeting { color: #ffd166; }
.t-type.vote { color: #7cc3ff; }
.t-type.speak { color: #9ee6b0; }
.t-type.end { color: #ff9f43; font-weight: 700; }
.t-text { color: #c6d0e8; }
.empty { color: #5a6278; text-align: center; padding: 30px 0; }
</style>
