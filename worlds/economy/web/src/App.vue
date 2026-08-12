<template>
  <div class="app">
    <header class="app-header">
      <div class="title">
        <span class="logo">💰</span>
        <span>AgentWorld · Economy</span>
        <span class="sub">虚拟经济世界观察台</span>
        <a class="repo" href="https://github.com/iwana888/AgentWorld" target="_blank" rel="noopener noreferrer"
           title="开源地址">
          <svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true">
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z"/>
          </svg>
          <span>github.com/iwana888/AgentWorld</span>
        </a>
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
.repo { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; color: #7cc3ff;
  text-decoration: none; padding: 2px 8px; border: 1px solid #2a3550; border-radius: 20px;
  background: #0e1322; transition: all .2s; }
.repo:hover { color: #9ed2ff; border-color: #3a5278; background: #131b2e; }
.meta { display: flex; gap: 6px; }
.app-body { flex: 1; display: grid; grid-template-columns: 0.8fr 1.1fr 0.9fr 0.9fr 1fr; gap: 10px; min-height: 0; }
.col-rank, .col-inspector, .col-tx, .col-sm, .col-lm { min-height: 0; overflow-y: auto; }
</style>
