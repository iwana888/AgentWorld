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

    <!-- 服务市场 -->
    <div class="lm-mkt-title">🛠️ 可雇佣服务</div>
    <div class="lm-services">
      <div v-for="s in snapshot.services" :key="s.id" class="svc-row">
        <span class="svc-emoji">{{ skillEmoji(s.skill) }}</span>
        <span class="svc-name">{{ s.name }}</span>
        <span class="svc-workers" :class="{ scarce: s.availableWorkers <= 2 }">
          {{ s.availableWorkers }} 人
        </span>
        <span class="svc-price">{{ s.price }}</span>
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
        <span class="ct-price">-{{ c.price }}</span>
      </div>
      <div v-if="!contracts.length" class="lm-empty">还没有雇佣活动…</div>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { skillEmoji } from '../types'
import type { GameSnapshot, ContractView } from '../types'

const props = defineProps<{ snapshot: GameSnapshot }>()

const contracts = computed<ContractView[]>(() => {
  const list = props.snapshot.contracts || []
  return [...list].slice(0, 40)
})

function statusIcon(s: string) {
  return s === 'completed' ? '✅' : s === 'failed' ? '❌' : '⏳'
}
function fmtVol(v: number) {
  if (v >= 1000) return (v / 1000).toFixed(1) + 'k'
  return String(v)
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
.svc-row { display: flex; align-items: center; gap: 6px; padding: 4px 6px; font-size: 11px; color: #c6d0e8; background: #161d33; border-radius: 6px; }
.svc-emoji { flex-shrink: 0; }
.svc-name { flex: 1; color: #e2e9ff; }
.svc-workers { color: #7cc3ff; font-size: 10px; flex-shrink: 0; }
.svc-workers.scarce { color: #ffd166; font-weight: 700; }
.svc-price { color: #ffd166; font-weight: 700; flex-shrink: 0; }
.lm-contracts { display: flex; flex-direction: column; gap: 3px; flex: 1; min-height: 0; overflow-y: auto; }
.ct-item { display: flex; align-items: center; gap: 4px; padding: 3px 6px; font-size: 10px; color: #c6d0e8; background: #161d33; border-radius: 6px; }
.ct-status { flex-shrink: 0; }
.ct-status.completed { filter: none; }
.ct-employer, .ct-worker { color: #7cc3ff; flex-shrink: 0; }
.ct-arrow { color: #5a6278; flex-shrink: 0; }
.ct-service { flex: 1; color: #c6d0e8; }
.ct-price { color: #ff7b72; font-weight: 700; flex-shrink: 0; }
.lm-empty { color: #5a6278; text-align: center; padding: 14px 0; font-size: 12px; }
</style>
