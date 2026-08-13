<template>
  <el-card class="lm" shadow="never">
    <template #header>
      <div class="lm-title">🤝 劳动力市场 <span class="lm-sub">Agent 互相雇佣</span></div>
    </template>

    <!-- 合约统计 -->
    <div class="lm-stats">
      <div class="stat-cell">
        <div class="stat-val">{{ snapshot.contractStats.total }}</div>
        <div class="stat-label">合约</div>
      </div>
      <div class="stat-cell good">
        <div class="stat-val">{{ snapshot.contractStats.completed }}</div>
        <div class="stat-label">成功</div>
      </div>
      <div class="stat-cell bad">
        <div class="stat-val">{{ snapshot.contractStats.failed }}</div>
        <div class="stat-label">失败</div>
      </div>
      <div class="stat-cell">
        <div class="stat-val">{{ fmtVol(snapshot.contractStats.totalVolume) }}</div>
        <div class="stat-label">成交额</div>
      </div>
    </div>

    <!-- 服务市场 + Worker 排名（M6.3 含声誉） -->
    <div class="lm-mkt-title">🛠️ 可雇佣服务</div>
    <div class="lm-services">
      <div v-for="s in snapshot.services" :key="s.id" class="svc-card">
        <div class="svc-row">
          <span class="svc-emoji">{{ skillEmoji(s.skill) }}</span>
          <span class="svc-name">{{ s.name }}</span>
          <span class="svc-workers" :class="{ scarce: s.availableWorkers <= 2 }">
            {{ s.availableWorkers }} 人
          </span>
          <span class="svc-price" :title="'基础价 ' + s.price">₿{{ s.price }}</span>
        </div>
        <!-- Worker 排名：可靠性优先 -->
        <div v-if="s.workers && s.workers.length" class="svc-workers-list">
          <div v-for="wk in topWorkers(s.workers)" :key="wk.agentID" class="worker-row">
            <span class="wk-name">{{ wk.name }}</span>
            <span class="wk-lv">Lv{{ wk.skillLevel }}</span>
            <span class="wk-rate" :class="rateClass(wk.successRate)">{{ pct(wk.successRate) }}</span>
            <span class="wk-rep" :class="repClass(wk.reputation)">♛{{ wk.reputation }}</span>
            <span class="wk-price" :class="priceClass(wk, s)">{{ wk.price }}</span>
          </div>
        </div>
      </div>
      <div v-if="!snapshot.services.length" class="lm-empty">暂无服务市场数据…</div>
    </div>

    <!-- 雇佣活动流 -->
    <div class="lm-mkt-title">📜 雇佣活动</div>
    <div class="lm-contracts">
      <div v-for="c in contracts" :key="c.id" class="ct-item">
        <span class="ct-status" :class="c.status">{{ statusIcon(c.status) }}</span>
        <span class="ct-employer">#{{ c.employer }}</span>
        <span class="ct-arrow">→</span>
        <span class="ct-worker">#{{ c.worker }}</span>
        <span class="ct-service">{{ c.service }}</span>
        <span v-if="c.status==='working'" class="ct-dur">~{{ c.duration }}s</span>
        <span class="ct-price">-{{ c.price }}</span>
      </div>
      <div v-if="!contracts.length" class="lm-empty">还没有雇佣活动…</div>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { skillEmoji } from '../types'
import type { GameSnapshot, ContractView, WorkerOffer } from '../types'

const props = defineProps<{ snapshot: GameSnapshot }>()

const contracts = computed<ContractView[]>(() => {
  const list = props.snapshot.contracts || []
  return [...list].slice(0, 40)
})

function statusIcon(s: string) {
  return s === 'completed' ? '✅' : s === 'failed' ? '❌' : '⏳'  // working → ⏳ 执行中
}
function fmtVol(v: number) {
  if (v >= 1000) return (v / 1000).toFixed(1) + 'k'
  return String(v)
}
// M6.3 每个服务只展示 top 3 最可靠的 worker（排名已按成功率降序）
function topWorkers(workers: WorkerOffer[]) {
  return [...workers].slice(0, 3)
}
function pct(r: number) {
  return Math.round(r * 100) + '%'
}
function rateClass(r: number) {
  return r >= 0.85 ? 'good' : r >= 0.7 ? 'mid' : 'bad'
}
function repClass(r: number) {
  return r >= 80 ? 'good' : r >= 50 ? 'mid' : 'bad'
}
// M6.4 价格对比：该 worker 报价 vs 服务基础价（高溢价 → 黄/红）
function priceClass(wk: WorkerOffer, s: any) {
  const base = s.price || 1
  const ratio = wk.price / base
  return ratio > 1.3 ? 'high' : ratio > 1.1 ? 'mid' : ''
}
</script>

<style scoped>
.lm { background: #121826; border: 1px solid #2a3550; display: flex; flex-direction: column; }
.lm-title { color: #e2e9ff; font-weight: 800; }
.lm-sub { font-size: 11px; color: #6b7696; font-weight: 400; margin-left: 6px; }
.lm-stats { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: 4px; margin-bottom: 8px; }
.stat-cell { background: #10162a; border: 1px solid #232f4a; border-radius: 6px; padding: 5px; text-align: center; }
.stat-val { font-size: 15px; font-weight: 800; color: #e2e9ff; }
.stat-cell.good .stat-val { color: #9ee6b0; }
.stat-cell.bad .stat-val { color: #ff7b72; }
.stat-label { font-size: 9px; color: #7a86a6; }
.lm-mkt-title { color: #9fb3e8; font-size: 12px; font-weight: 700; margin: 6px 0 4px; }
.lm-services { display: flex; flex-direction: column; gap: 3px; }
.svc-card { background: #161d33; border-radius: 6px; padding: 4px; }
.svc-row { display: flex; align-items: center; gap: 6px; padding: 3px 6px; font-size: 11px; color: #c6d0e8; }
.svc-emoji { flex-shrink: 0; }
.svc-name { flex: 1; color: #e2e9ff; }
.svc-workers { color: #7cc3ff; font-size: 10px; flex-shrink: 0; }
.svc-workers.scarce { color: #ffd166; font-weight: 700; }
.svc-price { color: #ffd166; font-weight: 700; flex-shrink: 0; }
/* M6.3 Worker 排名 */
.svc-workers-list { display: flex; flex-direction: column; gap: 1px; margin-top: 2px; border-top: 1px dashed #2a3550; padding-top: 2px; }
.worker-row { display: flex; align-items: center; gap: 6px; padding: 1px 8px; font-size: 10px; color: #9aa6c5; }
.wk-name { flex: 1; color: #c6d0e8; }
.wk-lv { color: #7cc3ff; flex-shrink: 0; }
.wk-rate { width: 36px; text-align: right; flex-shrink: 0; font-weight: 700; }
.wk-rate.good { color: #9ee6b0; }
.wk-rate.mid { color: #e5c07b; }
.wk-rate.bad { color: #ff7b72; }
.wk-rep { width: 36px; text-align: right; flex-shrink: 0; }
.wk-rep.good { color: #ffd166; }
.wk-rep.mid { color: #e5c07b; }
.wk-rep.bad { color: #ff7b72; }
/* M6.4 独立报价 */
.wk-price { width: 30px; text-align: right; flex-shrink: 0; color: #7cc3ff; font-weight: 700; }
.wk-price.high { color: #ffd166; }
.wk-price.mid { color: #e5c07b; }
.lm-contracts { display: flex; flex-direction: column; gap: 3px; flex: 1; min-height: 0; overflow-y: auto; }
.ct-item { display: flex; align-items: center; gap: 4px; padding: 3px 6px; font-size: 10px; color: #c6d0e8; background: #161d33; border-radius: 6px; }
.ct-status { flex-shrink: 0; }
.ct-status.completed { filter: none; }
.ct-employer, .ct-worker { color: #7cc3ff; flex-shrink: 0; }
.ct-arrow { color: #5a6278; flex-shrink: 0; }
.ct-service { flex: 1; color: #c6d0e8; }
.ct-dur { color: #7a86a6; font-size: 9px; flex-shrink: 0; }
.ct-price { color: #ff7b72; font-weight: 700; flex-shrink: 0; }
.lm-empty { color: #5a6278; text-align: center; padding: 14px 0; font-size: 12px; }
</style>
