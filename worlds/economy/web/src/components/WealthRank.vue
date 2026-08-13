<template>
  <el-card class="rank" shadow="never">
    <template #header>
      <div class="rank-title">👑 财富榜 <span class="rank-sub">点击查看 Agent 决策</span></div>
    </template>
    <div class="rank-list">
      <div v-for="(a, i) in sortedAgents" :key="a.id" class="rank-item"
        :class="{ active: selected === a.id }" @click="$emit('select', a.id)">
        <span class="rank-no" :class="{ top: i < 3 }">{{ i + 1 }}</span>
        <span class="rank-emoji">{{ emoji(a.profession) }}</span>
        <span class="rank-name">{{ a.name }}</span>
        <span class="rank-prof">{{ a.profession }}</span>
        <el-progress :percentage="wealthPct(a.balance)" :stroke-width="6" :show-text="false"
          class="rank-bar" :color="barColor(i)" />
        <span v-if="skillCount(a) > 1" class="rank-skills" :title="skillNames(a)">🎓×{{ skillCount(a) }}</span>
        <span v-if="a.reputation > 0" class="rank-rep" :title="'成功率 ' + Math.round((a.successRate||0)*100) + '%'">♛{{ a.reputation }}</span>
        <span class="rank-bal">{{ a.balance.toLocaleString() }}</span>
      </div>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { profEmoji } from '../types'
import type { AgentPublic } from '../types'

const props = defineProps<{ agents: AgentPublic[]; selected: number | null }>()
defineEmits<{ (e: 'select', id: number): void }>()

const sortedAgents = computed(() => [...props.agents].sort((a, b) => b.balance - a.balance))
const maxBal = computed(() => Math.max(1, ...props.agents.map(a => a.balance)))

function wealthPct(b: number) { return Math.max(2, Math.round((b / maxBal.value) * 100)) }
function barColor(i: number) {
  return ['#ffd166', '#e5c07b', '#b07ce0'][i] || '#3d5278'
}
function emoji(p: string) { return profEmoji(p) }
function skillCount(a: AgentPublic) { return (a.skills || []).filter(s => s.level > 0).length }
function skillNames(a: AgentPublic) {
  return (a.skills || []).filter(s => s.level > 0).map(s => `${s.skillID} Lv${s.level}`).join(', ')
}
</script>

<style scoped>
.rank { background: #121826; border: 1px solid #2a3550; }
.rank-title { color: #e2e9ff; font-weight: 800; }
.rank-sub { font-size: 11px; color: #6b7696; font-weight: 400; margin-left: 8px; }
.rank-list { display: flex; flex-direction: column; gap: 4px; }
.rank-item { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 8px; cursor: pointer; }
.rank-item:hover { background: #182238; }
.rank-item.active { background: #1d2a48; outline: 1px solid #3d5278; }
.rank-no { width: 18px; color: #6b7696; font-size: 12px; text-align: center; flex-shrink: 0; }
.rank-no.top { color: #ffd166; font-weight: 800; }
.rank-emoji { font-size: 16px; flex-shrink: 0; }
.rank-name { color: #c6d0e8; font-size: 13px; font-weight: 600; width: 56px; flex-shrink: 0; }
.rank-prof { color: #7a86a6; font-size: 11px; width: 52px; flex-shrink: 0; }
.rank-bar { flex: 1; }
.rank-skills { color: #b07ce0; font-size: 11px; flex-shrink: 0; }
.rank-rep { color: #ffd166; font-size: 10px; flex-shrink: 0; font-weight: 700; }
.rank-bal { color: #ffd166; font-size: 13px; font-weight: 700; width: 60px; text-align: right; flex-shrink: 0; font-variant-numeric: tabular-nums; }
</style>
