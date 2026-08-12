<template>
  <div class="app">
    <header class="app-header">
      <div class="title">
        <span class="logo">💰</span>
        <span>AgentWorld · Economy</span>
        <span class="sub">虚拟经济世界观察台</span>
      </div>
      <div class="meta">
        <el-tag size="small" effect="dark">Round {{ snapshot.round }}</el-tag>
        <el-tag size="small" effect="dark" type="warning">{{ snapshot.agents.length }} Agents</el-tag>
        <el-tag :type="connected ? 'success' : 'danger'" size="small" effect="plain">
          {{ connected ? '● 实时' : '○ 断开' }}
        </el-tag>
      </div>
    </header>

    <!-- 顶部经济面板 -->
    <EconomyPanel :snapshot="snapshot" />

    <!-- 主体：财富榜 / Inspector / 技能市场 / 劳动力市场 / 交易流 -->
    <div class="app-body">
      <div class="col-rank">
        <WealthRank :agents="snapshot.agents" :selected="selected" @select="onSelect" />
      </div>
      <div class="col-inspector">
        <AgentInspector :agent="selectedAgent" :load-inspector="fetchInspector" />
      </div>
      <div class="col-sm">
        <SkillMarket :snapshot="snapshot" :stream="txStream" />
      </div>
      <div class="col-lm">
        <LaborMarket :snapshot="snapshot" />
      </div>
      <div class="col-tx">
        <TxStream :recent-tx="snapshot.recentTx" :tx-stream="txStream" :connected="connected" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import EconomyPanel from './components/EconomyPanel.vue'
import WealthRank from './components/WealthRank.vue'
import AgentInspector from './components/AgentInspector.vue'
import TxStream from './components/TxStream.vue'
import SkillMarket from './components/SkillMarket.vue'
import LaborMarket from './components/LaborMarket.vue'
import { useGame } from './composables/useGame'

const { snapshot, txStream, connected, fetchInspector } = useGame()

const selected = ref<number | null>(null)
function onSelect(id: number) {
  selected.value = selected.value === id ? null : id
}
const selectedAgent = computed(() => {
  if (!selected.value) return null
  return snapshot.agents.find(a => a.id === selected.value) ?? null
})
</script>

<style>
.app { display: flex; flex-direction: column; height: 100vh; padding: 12px; gap: 12px; }
.app-header { display: flex; align-items: center; justify-content: space-between; color: #e2e9ff; }
.title { font-size: 18px; font-weight: 800; display: flex; align-items: center; gap: 8px; }
.logo { font-size: 22px; }
.sub { font-size: 12px; color: #7a86a6; font-weight: 400; }
.meta { display: flex; gap: 6px; }
.app-body { flex: 1; display: grid; grid-template-columns: 0.8fr 1.1fr 0.9fr 0.9fr 1fr; gap: 10px; min-height: 0; }
.col-rank, .col-inspector, .col-tx, .col-sm, .col-lm { min-height: 0; overflow-y: auto; }
</style>
