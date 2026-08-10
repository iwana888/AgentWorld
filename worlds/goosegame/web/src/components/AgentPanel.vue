<template>
  <el-card class="agent-panel" shadow="never">
    <template #header>
      <div class="panel-header">
        <div class="ptitle">
          <span v-if="agent">{{ emoji(agent.team) }} {{ agent.name }}</span>
          <span v-else>Agent Inspector</span>
          <el-tag v-if="agent" :type="agent.alive ? 'success' : 'danger'" size="small">
            {{ agent.alive ? '存活' : '已淘汰' }}
          </el-tag>
        </div>
        <el-tag v-if="agent" size="small" effect="plain" class="tag-debug">Inspector</el-tag>
      </div>
    </template>

    <!-- 未选中 -->
    <div v-if="!agent" class="empty">
      <div class="empty-emoji">👆</div>
      <p>点击地图上的角色查看它的内心世界</p>
      <p class="sub">（游戏 UI 与 Agent 内部状态分层：普通观众只看到行为，点开角色才看到"为什么"）</p>
    </div>

    <!-- 加载中 -->
    <div v-else-if="loading" class="loading">读取内心状态…</div>

    <!-- Inspector 数据 -->
    <div v-else-if="inspector" class="inspector">
      <div class="sec">
        <div class="sec-label">🎯 目标</div>
        <div class="sec-val">{{ inspector.goal }}</div>
      </div>

      <div class="sec">
        <div class="sec-label">🧠 最近决策</div>
        <div class="sec-val">{{ inspector.lastDecision || '—' }}</div>
        <div class="sec-val sub">{{ inspector.lastAction || '' }}</div>
      </div>

      <div class="sec">
        <div class="sec-label">🤔 怀疑（Belief）</div>
        <div class="bars">
          <div v-for="b in beliefSorted" :key="b.agentID" class="bar-row">
            <span class="bar-name">{{ emojiOf(b.agentID) }} {{ b.name }}</span>
            <el-progress :percentage="Math.round(b.suspicion * 100)" :stroke-width="8"
              :color="barColor(b.suspicion)" :show-text="false" class="bar" />
            <span class="bar-val">{{ b.suspicion.toFixed(2) }}</span>
          </div>
          <div v-if="inspector.belief.length === 0" class="no-data">还没有明确怀疑</div>
        </div>
      </div>

      <div class="sec">
        <div class="sec-label">❤️ 关系（Relationship）</div>
        <div class="rels">
          <div v-for="r in relSorted" :key="r.agentID" class="rel-row">
            <span class="rel-name">{{ emojiOf(r.agentID) }} {{ r.name }}</span>
            <span class="rel-val" :class="relClass(r.goodwill)">
              {{ r.goodwill >= 0 ? '+' : '' }}{{ r.goodwill.toFixed(2) }}
            </span>
          </div>
          <div v-if="inspector.relationship.length === 0" class="no-data">还没有明显关系</div>
        </div>
      </div>

      <div class="sec">
        <div class="sec-label">📖 记忆（最近事件）</div>
        <div class="memory">
          <div v-for="(m, i) in inspector.memory" :key="i" class="mem-item">{{ m }}</div>
          <div v-if="inspector.memory.length === 0" class="no-data">暂无记忆</div>
        </div>
      </div>
    </div>

    <div v-else class="empty">无法读取该 Agent 的状态</div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import type { AgentPublic, InspectorData } from '../types'
import { teamEmoji } from '../types'

const props = defineProps<{
  agent: AgentPublic | null
  agents: AgentPublic[]
  loadInspector: (id: number) => Promise<InspectorData | null>
}>()

const inspector = ref<InspectorData | null>(null)
const loading = ref(false)

watch(() => props.agent?.id, async (id) => {
  inspector.value = null
  if (!id) return
  loading.value = true
  inspector.value = await props.loadInspector(id)
  loading.value = false
}, { immediate: true })

const beliefSorted = computed(() => [...(inspector.value?.belief || [])].sort((a, b) => b.suspicion - a.suspicion))
const relSorted = computed(() => [...(inspector.value?.relationship || [])].sort((a, b) => b.goodwill - a.goodwill))

function emoji(team: string) {
  return teamEmoji(team)
}
function emojiOf(id: number) {
  const a = props.agents.find(x => x.id === id)
  return teamEmoji(a?.team || 'Goose')
}
function barColor(v: number) {
  return v >= 0.6 ? '#ff6b6b' : v >= 0.3 ? '#ffd166' : '#7cc3ff'
}
function relClass(v: number) {
  return v >= 0.15 ? 'trust' : v <= -0.15 ? 'enemy' : 'neutral'
}
</script>

<style scoped>
.agent-panel { height: 100%; background: #121826; border: 1px solid #2a3550; }
.panel-header { display: flex; align-items: center; justify-content: space-between; color: #e2e9ff; font-weight: 700; }
.ptitle { display: flex; align-items: center; gap: 8px; }
.tag-debug { color: #e5c07b; border-color: #e5c07b; }
.empty { color: #6b7696; text-align: center; padding: 26px 10px; }
.empty-emoji { font-size: 32px; margin-bottom: 8px; }
.empty .sub { font-size: 11px; color: #4f5b78; margin-top: 8px; line-height: 1.6; }
.loading { color: #7a86a6; text-align: center; padding: 30px 0; }

.inspector { display: flex; flex-direction: column; gap: 12px; overflow-y: auto; max-height: 100%; }
.sec { background: #182238; border: 1px solid #232f4a; border-radius: 10px; padding: 10px 12px; }
.sec-label { color: #9fb3e8; font-size: 12px; font-weight: 700; margin-bottom: 6px; }
.sec-val { color: #c6d0e8; font-size: 13px; }
.sec-val.sub { color: #7a86a6; font-size: 11px; margin-top: 2px; }

.bars { display: flex; flex-direction: column; gap: 6px; }
.bar-row { display: flex; align-items: center; gap: 8px; }
.bar-name { color: #c6d0e8; font-size: 12px; width: 70px; flex-shrink: 0; }
.bar { flex: 1; }
.bar-val { color: #8b98b8; font-size: 11px; width: 32px; text-align: right; flex-shrink: 0; }

.rels { display: flex; flex-wrap: wrap; gap: 6px; }
.rel-row { display: flex; align-items: center; gap: 6px; background: #1a2440; padding: 4px 8px; border-radius: 8px; font-size: 12px; }
.rel-name { color: #c6d0e8; }
.rel-val { font-weight: 700; }
.rel-val.trust { color: #7cc3ff; }
.rel-val.enemy { color: #ff7b72; }
.rel-val.neutral { color: #8b98b8; }

.memory { display: flex; flex-direction: column; gap: 4px; }
.mem-item { font-size: 11px; color: #aeb9d6; padding: 3px 0; border-bottom: 1px dashed #232f4a; }
.no-data { color: #5a6278; font-size: 12px; }
</style>
