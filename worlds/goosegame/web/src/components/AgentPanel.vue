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
        <div class="sec-label">🎭 性格</div>
        <div class="sec-val">{{ inspector.personality || '—' }}</div>
      </div>

      <div class="sec">
        <div class="sec-label">🎯 当前目标</div>
        <div class="sec-val">{{ inspector.goal }}</div>
      </div>

      <div class="sec">
        <div class="sec-label">🧠 最近决策</div>
        <div class="sec-val decision">{{ inspector.lastDecision || '—' }}</div>
        <div class="sec-val sub">{{ inspector.lastAction || '' }}</div>
      </div>

      <div class="sec why">
        <div class="sec-label">💭 为什么这么做</div>
        <div v-if="whyLines.length" class="why-lines">
          <div v-for="(line, i) in whyLines" :key="i" class="why-line"
            :class="{ 'why-act': line.key === '因此' }">
            <span class="why-key">{{ line.key }}</span>
            <span class="why-val">{{ line.val }}</span>
          </div>
        </div>
        <div v-else class="no-data">还没有决策记录</div>
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

      <!-- M8：决策历史（DecisionRecord）——展示这个 Agent 一步步怎么从"看到"走到"行动" -->
      <div class="sec" v-if="inspector.decisions && inspector.decisions.length">
        <div class="sec-label">🕘 决策历史（DecisionRecord）</div>
        <div class="dec-list">
          <div v-for="(dc, i) in inspector.decisions.slice(0, 8)" :key="i"
            class="dec-item" :class="{ open: openDec === i }" @click="openDec = openDec === i ? -1 : i">
            <div class="dec-head">
              <span class="dec-time">{{ timeStr(dc.timestamp) }}</span>
              <span class="dec-what">{{ dc.decision }}</span>
              <span class="dec-toggle">{{ openDec === i ? '▾' : '▸' }}</span>
            </div>
            <div v-if="openDec === i" class="dec-body">
              <div class="dec-row"><span class="dec-k">目标</span><span class="dec-v">{{ dc.goal }}</span></div>
              <div class="dec-row"><span class="dec-k">看到</span><span class="dec-v">{{ dc.perception }}</span></div>
              <div v-if="dc.memory" class="dec-row"><span class="dec-k">记忆</span><span class="dec-v">{{ dc.memory }}</span></div>
              <div v-if="dc.relationship" class="dec-row"><span class="dec-k">关系</span><span class="dec-v">{{ dc.relationship }}</span></div>
              <div class="dec-row"><span class="dec-k">因此</span><span class="dec-v act">{{ dc.decision }}</span></div>
              <div v-if="dc.outcome" class="dec-row"><span class="dec-k">结果</span><span class="dec-v">{{ dc.outcome }}</span></div>
            </div>
          </div>
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
const openDec = ref(-1)   // 决策历史中当前展开的条目

function timeStr(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

watch(() => props.agent?.id, async (id) => {
  inspector.value = null
  if (!id) return
  loading.value = true
  inspector.value = await props.loadInspector(id)
  loading.value = false
}, { immediate: true })

const beliefSorted = computed(() => [...(inspector.value?.belief || [])].sort((a, b) => b.suspicion - a.suspicion))
const relSorted = computed(() => [...(inspector.value?.relationship || [])].sort((a, b) => b.goodwill - a.goodwill))

// 解析"为什么"文本（后端格式：性格：…\n目标：…\n看到：…\n记忆：…\n因此：…）
const whyLines = computed(() => {
  const why = inspector.value?.lastWhy
  if (!why) return []
  return why.split('\n').filter(Boolean).map(line => {
    const idx = line.indexOf('：')
    const key = idx > 0 ? line.slice(0, idx) : ''
    const val = idx > 0 ? line.slice(idx + 1) : line
    return { key, val }
  })
})

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
.sec-val.decision { color: #ffd166; font-weight: 700; }

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

.why { background: linear-gradient(180deg, #1b2440, #182238); border-color: #2f3d5e; }
.why-lines { display: flex; flex-direction: column; gap: 5px; }
.why-line { font-size: 12px; display: flex; gap: 6px; line-height: 1.5; }
.why-key { color: #7cc3ff; font-weight: 700; flex-shrink: 0; }
.why-val { color: #c6d0e8; }
.why-line.why-act { background: #243050; border-radius: 6px; padding: 4px 8px; margin-top: 2px; }
.why-line.why-act .why-key { color: #ffd166; }
.why-line.why-act .why-val { color: #ffd166; font-weight: 700; }

.dec-list { display: flex; flex-direction: column; gap: 4px; }
.dec-item { background: #16203a; border: 1px solid #232f4a; border-radius: 8px; overflow: hidden; cursor: pointer; }
.dec-item.open { border-color: #3d5278; }
.dec-head { display: flex; align-items: center; gap: 8px; padding: 6px 8px; font-size: 12px; }
.dec-time { color: #6b7696; font-size: 10px; flex-shrink: 0; font-variant-numeric: tabular-nums; }
.dec-what { color: #c6d0e8; flex: 1; }
.dec-toggle { color: #6b7696; }
.dec-body { padding: 6px 10px 8px; border-top: 1px dashed #2a3550; display: flex; flex-direction: column; gap: 4px; }
.dec-row { font-size: 11px; display: flex; gap: 8px; line-height: 1.5; }
.dec-k { color: #7cc3ff; font-weight: 700; width: 40px; flex-shrink: 0; }
.dec-v { color: #c6d0e8; }
.dec-v.act { color: #ffd166; font-weight: 700; }
</style>
