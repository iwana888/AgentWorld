<template>
  <div class="timeline">
    <div class="timeline-header">
      <span>实时事件</span>
      <el-tag :type="connected ? 'success' : 'danger'" size="small">
        {{ connected ? '已连接' : '断开' }}
      </el-tag>
    </div>
    <el-scrollbar class="timeline-scroll" ref="scrollRef">
      <div v-for="(ev, i) in events" :key="i" class="timeline-item"
        :class="{ clickable: agentOf(ev) != null }"
        :title="agentOf(ev) != null ? '点击查看为什么' : ''"
        @click="onClick(ev)">
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
const emit = defineEmits<{ (e: 'explain', agentId: number): void }>()
const scrollRef = ref()

// 事件是否有对应的 Agent（可点开看"为什么"）
function agentOf(ev: ObsEvent): number | null {
  const id = ev.data?.agent ?? ev.data?.victim ?? ev.data?.agentID
  return typeof id === 'number' ? id : null
}
function onClick(ev: ObsEvent) {
  const id = agentOf(ev)
  if (id != null) emit('explain', id)
}

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
// M6 叙事化：把原始事件翻译成"Agent 带着意图行动"的句子，
// 让用户看到的不只是移动/发言，而是"这个 Agent 在决定做什么"。
function textFor(ev: ObsEvent) {
  const d = ev.data
  switch (ev.type) {
    case 'agent.moved': return `${d.name} 决定去 ${d.toRoom || d.to}`
    case 'task.completed': return `${d.name} 专注完成了 ${d.room} 的任务`
    case 'agent.killed': return `${d.name} 在 ${d.room} 被杀害`
    case 'body.found': return `发现尸体：${d.name} 死在 ${d.room}`
    case 'meeting.started': return `🚨 紧急会议（${d.reason}）`
    case 'agent.spoke': return `${d.name}：「${d.text}」`
    case 'vote.cast': return `${d.name} 把票投给了${d.targetName ? ' ' + d.targetName : '某人'}`
    case 'agent.eliminated': return `${d.name} 被公投淘汰（${d.team}）`
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
.timeline-item.clickable { cursor: pointer; border-radius: 6px; padding-left: 4px; padding-right: 4px; }
.timeline-item.clickable:hover { background: #1b2640; }
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
