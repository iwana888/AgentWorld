<template>
  <div class="eco-panel">
    <div class="stat-box">
      <div class="stat-label">💰 总资产</div>
      <div class="stat-value">{{ snapshot.totalWealth.toLocaleString() }} coins</div>
    </div>
    <div class="stat-box">
      <div class="stat-label">🕒 回合</div>
      <div class="stat-value">Round {{ snapshot.round }}</div>
    </div>
    <div class="stat-box">
      <div class="stat-label">🧑 经济体</div>
      <div class="stat-value">{{ snapshot.agents.length }} Agents</div>
    </div>
    <div class="stat-box jobs">
      <div class="stat-label">📋 开放工作</div>
      <div class="jobs-list">
        <el-tag v-for="j in snapshot.openJobs.slice(0, 6)" :key="j.id" size="small" class="job-tag"
          :type="jobType(j.skill)">
          {{ j.title }} · +{{ j.reward }}
        </el-tag>
        <span v-if="!snapshot.openJobs.length" class="no-jobs">暂无</span>
      </div>
    </div>
    <div class="stat-box market">
      <div class="stat-label">📈 市场价格</div>
      <div class="prices">
        <span v-for="(p, name) in priceList" :key="name" class="price-chip">
          {{ name }} {{ p }}
        </span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { GameSnapshot } from '../types'

const props = defineProps<{ snapshot: GameSnapshot }>()

const priceList = computed(() => {
  return Object.entries(props.snapshot.prices).sort((a, b) => b[1] - a[1]).slice(0, 8)
})

function jobType(skill: string) {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'danger' | 'info'> = {
    engineer: 'primary', farmer: 'success', courier: 'warning',
    doctor: 'danger', miner: 'info', chef: 'warning',
  }
  return map[skill] || 'info'
}
</script>

<style scoped>
.eco-panel { display: flex; gap: 12px; padding: 10px 14px; background: #10162a;
  border: 1px solid #2a3550; border-radius: 12px; flex-wrap: wrap; }
.stat-box { min-width: 120px; }
.stat-label { font-size: 11px; color: #7a86a6; margin-bottom: 4px; }
.stat-value { font-size: 16px; font-weight: 800; color: #ffd166; }
.stat-box.jobs { flex: 1.5; min-width: 240px; }
.stat-box.market { flex: 2; min-width: 260px; }
.jobs-list { display: flex; flex-wrap: wrap; gap: 4px; }
.job-tag { margin-right: 0; }
.no-jobs { color: #5a6278; font-size: 12px; }
.prices { display: flex; flex-wrap: wrap; gap: 6px; }
.price-chip { font-size: 11px; color: #aeb9d6; background: #1a2440; padding: 2px 7px; border-radius: 6px; }
</style>
