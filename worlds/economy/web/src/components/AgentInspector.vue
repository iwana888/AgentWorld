<template>
  <el-card class="inspector" shadow="never">
    <template #header>
      <div class="i-header">
        <span v-if="agent" class="i-title">{{ emoji(agent.profession) }} {{ agent.name }}
          <el-tag size="small" effect="plain">{{ agent.profession }}</el-tag>
        </span>
        <span v-else class="i-title">Agent Brain</span>
      </div>
    </template>

    <div v-if="!agent" class="i-empty">
      <div class="i-emoji">👆</div>
      <p>点击财富榜上的 Agent，查看它的经济决策</p>
      <p class="sub">为什么接这份工作？为什么买这个商品？为什么选择观望？</p>
    </div>

    <div v-else-if="loading" class="i-loading">读取决策…</div>

    <div v-else-if="inspector" class="i-body">
      <!-- 经济状态 -->
      <div class="row">
        <div class="cell">
          <div class="cell-label">💰 余额</div>
          <div class="cell-value gold">{{ inspector.balance.toLocaleString() }}</div>
        </div>
        <div class="cell">
          <div class="cell-label">📈 累计赚</div>
          <div class="cell-value green">{{ inspector.totalEarned.toLocaleString() }}</div>
        </div>
        <div class="cell">
          <div class="cell-label">📉 累计花</div>
          <div class="cell-value red">{{ inspector.totalSpent.toLocaleString() }}</div>
        </div>
      </div>

      <!-- M6.3 职业信誉 -->
      <div class="row" v-if="inspector.reputation > 0 || (inspector.completedContracts || 0) > 0">
        <div class="cell">
          <div class="cell-label">♛ 职业信誉</div>
          <div class="cell-value" :class="repClass(inspector.reputation)">{{ inspector.reputation || 0 }}</div>
        </div>
        <div class="cell">
          <div class="cell-label">🤝 完成/失败</div>
          <div class="cell-value">{{ inspector.completedContracts || 0 }} / {{ inspector.failedContracts || 0 }}</div>
        </div>
        <div class="cell">
          <div class="cell-label">✅ 成功率</div>
          <div class="cell-value" :class="rateClass(inspector.successRate)">{{ pct(inspector.successRate) }}</div>
        </div>
      </div>

      <div class="sec">
        <div class="sec-label">🎯 经济目标</div>
        <div class="sec-val">{{ inspector.goal }}</div>
      </div>

      <div class="sec">
        <div class="sec-label">🎭 性格</div>
        <div class="sec-val">{{ inspector.personality || '—' }}</div>
      </div>

      <div class="sec">
        <div class="sec-label">💭 为什么这么做（最近决策）</div>
        <div class="why-lines">
          <div v-for="(line, i) in whyParsed" :key="i" class="why-line" :class="{ act: line.key === '因此' }">
            <span class="why-key">{{ line.key }}</span>
            <span class="why-val">{{ line.val }}</span>
          </div>
          <div v-if="!whyParsed.length" class="no-data">暂无决策记录</div>
        </div>
      </div>

      <div class="sec" v-if="invList.length">
        <div class="sec-label">📦 库存</div>
        <div class="inv">
          <el-tag v-for="[name, q] in invList" :key="name" size="small" class="inv-tag">{{ name }} ×{{ q }}</el-tag>
        </div>
      </div>

      <div class="sec" v-if="skills.length">
        <div class="sec-label">🛠️ 技能（Skill System）</div>
        <div class="skills">
          <div v-for="s in skills" :key="s.skillID" class="skill-item">
            <span class="skill-name">{{ s.skillID }} <span class="skill-lv">Lv{{ s.level }}</span></span>
            <el-progress :percentage="skillPct(s.level)" :stroke-width="6" class="skill-bar" :show-text="false"
              :color="skillColor(s.level)" />
          </div>
        </div>
      </div>

      <div class="sec" v-if="inspector.skillInvested > 0">
        <div class="sec-label">📈 技能投资回报（M5 Skill Economy）</div>
        <div class="inv-metrics">
          <div class="inv-cell">
            <div class="inv-label">投入</div>
            <div class="inv-val red">{{ inspector.skillInvested.toLocaleString() }}</div>
          </div>
          <div class="inv-cell">
            <div class="inv-label">技能赚</div>
            <div class="inv-val green">{{ inspector.skillEarned.toLocaleString() }}</div>
          </div>
          <div class="inv-cell">
            <div class="inv-label">净回报</div>
            <div class="inv-val" :class="invReturnClass">{{ inspector.skillReturn.toLocaleString() }}</div>
          </div>
        </div>
        <div class="inv-verdict" :class="invVerdictClass">{{ invVerdict }}</div>
      </div>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { profEmoji } from '../types'
import type { AgentPublic, InspectorData } from '../types'

const props = defineProps<{
  agent: AgentPublic | null
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

const whyParsed = computed(() => {
  const why = inspector.value?.lastWhy
  if (!why) return []
  return why.split('\n').filter(Boolean).map(line => {
    const idx = line.indexOf('：')
    const key = idx > 0 ? line.slice(0, idx) : ''
    const val = idx > 0 ? line.slice(idx + 1) : line
    return { key, val }
  })
})
const invList = computed(() => Object.entries(inspector.value?.inventory || {}).filter(([, q]) => q > 0))
const skills = computed(() => (inspector.value?.skills || []).filter(s => s.level > 0).sort((a, b) => b.level - a.level))

const invReturnClass = computed(() => {
  const r = inspector.value?.skillReturn ?? 0
  return r > 0 ? 'green' : r < 0 ? 'red' : ''
})
const invVerdict = computed(() => {
  const invested = inspector.value?.skillInvested ?? 0
  const ret = inspector.value?.skillReturn ?? 0
  if (invested <= 0) return ''
  if (ret >= invested) return '✅ 投资成功：技能投入已回本并盈利'
  if (ret > 0) return '⏳ 投资回收中：已部分回本'
  return '⚠️ 投资亏损：尚未回本（技能投资有风险）'
})
const invVerdictClass = computed(() => {
  const invested = inspector.value?.skillInvested ?? 0
  const ret = inspector.value?.skillReturn ?? 0
  if (invested <= 0) return ''
  if (ret >= invested) return 'good'
  if (ret > 0) return 'mid'
  return 'bad'
})

function emoji(p: string) { return profEmoji(p) }
function skillPct(level: number) { return Math.round((level / 7) * 100) }
function skillColor(level: number) {
  return level >= 6 ? '#ffd166' : level >= 4 ? '#7cc3ff' : '#3d5278'
}
// M6.3 职业信誉展示
function repClass(r: number) {
  const v = r || 0
  return v >= 80 ? 'green' : v >= 50 ? 'gold' : 'red'
}
function rateClass(r: number) {
  const v = r || 0
  return v >= 0.85 ? 'green' : v >= 0.7 ? 'gold' : 'red'
}
function pct(r: number) {
  return Math.round((r || 0) * 100) + '%'
}
</script>

<style scoped>
.inspector { background: #121826; border: 1px solid #2a3550; }
.i-header { color: #e2e9ff; font-weight: 800; }
.i-empty { color: #6b7696; text-align: center; padding: 24px 10px; }
.i-emoji { font-size: 30px; margin-bottom: 8px; }
.i-empty .sub { font-size: 11px; color: #4f5b78; margin-top: 6px; }
.i-loading { color: #7a86a6; text-align: center; padding: 30px 0; }

.i-body { display: flex; flex-direction: column; gap: 10px; overflow-y: auto; max-height: calc(100vh - 280px); }
.row { display: flex; gap: 8px; }
.cell { flex: 1; background: #182238; border: 1px solid #232f4a; border-radius: 8px; padding: 8px; text-align: center; }
.cell-label { font-size: 11px; color: #7a86a6; }
.cell-value { font-size: 14px; font-weight: 800; margin-top: 2px; }
.gold { color: #ffd166; }
.green { color: #9ee6b0; }
.red { color: #ff7b72; }

.sec { background: #182238; border: 1px solid #232f4a; border-radius: 10px; padding: 10px 12px; }
.sec-label { color: #9fb3e8; font-size: 12px; font-weight: 700; margin-bottom: 6px; }
.sec-val { color: #c6d0e8; font-size: 13px; }

.why-lines { display: flex; flex-direction: column; gap: 5px; }
.why-line { font-size: 12px; display: flex; gap: 6px; line-height: 1.5; }
.why-key { color: #7cc3ff; font-weight: 700; flex-shrink: 0; }
.why-val { color: #c6d0e8; }
.why-line.act { background: #243050; border-radius: 6px; padding: 4px 8px; margin-top: 2px; }
.why-line.act .why-key, .why-line.act .why-val { color: #ffd166; font-weight: 700; }

.inv { display: flex; flex-wrap: wrap; gap: 4px; }
.inv-tag { margin-right: 0; }
.skills { display: flex; flex-direction: column; gap: 5px; }
.skill-item { display: flex; align-items: center; gap: 8px; }
.skill-name { color: #c6d0e8; font-size: 11px; width: 76px; flex-shrink: 0; text-transform: capitalize; }
.skill-lv { color: #7cc3ff; font-weight: 700; margin-left: 3px; }
.skill-bar { flex: 1; }
.no-data { color: #5a6278; font-size: 12px; }
.inv-metrics { display: flex; gap: 6px; }
.inv-cell { flex: 1; background: #10162a; border: 1px solid #232f4a; border-radius: 6px; padding: 6px; text-align: center; }
.inv-label { font-size: 10px; color: #7a86a6; }
.inv-val { font-size: 14px; font-weight: 800; margin-top: 2px; }
.inv-verdict { margin-top: 6px; font-size: 11px; padding: 5px 8px; border-radius: 6px; }
.inv-verdict.good { color: #9ee6b0; background: #14341f; }
.inv-verdict.mid { color: #e5c07b; background: #332a14; }
.inv-verdict.bad { color: #ff7b72; background: #3a1c1c; }
</style>
