<template>
  <el-card class="agent-panel" shadow="never">
    <template #header>
      <div class="panel-header">
        <span v-if="agent">{{ agent.name }}</span>
        <span v-else>Agent 详情</span>
        <el-tag v-if="agent" :type="agent.alive ? '' : 'danger'" size="small">
          {{ agent.alive ? '存活' : '已淘汰' }}
        </el-tag>
      </div>
    </template>

    <div v-if="agent" class="agent-info">
      <el-descriptions :column="1" size="small" border>
        <el-descriptions-item label="身份">
          <span :class="teamClass(agent.team)">{{ teamLabel(agent.team) }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="位置">{{ agent.room }}</el-descriptions-item>
        <el-descriptions-item label="任务进度">{{ agent.taskDone }}</el-descriptions-item>
      </el-descriptions>

      <p class="hint">（Belief / Relationship 是 Agent 的私有主观状态，仅向观战者提供"为什么"接口时展示——M5 v0.1 暂不开放。）</p>
    </div>
    <div v-else class="empty">
      <p>点击地图上的 Agent 查看公开信息</p>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { AgentPublic } from '../types'

const props = defineProps<{ agent: AgentPublic | null }>()

function teamClass(t: string) {
  return t === 'Duck' ? 'team-duck' : t === 'Dodo' ? 'team-dodo' : 'team-goose'
}
function teamLabel(t: string) {
  return t === 'Duck' ? '🦆 鸭' : t === 'Dodo' ? '🦤 中立' : '🦢 鹅'
}
</script>

<style scoped>
.agent-panel { height: 100%; background: #121826; border: 1px solid #2a3550; }
.panel-header { display: flex; align-items: center; justify-content: space-between; color: #e2e9ff; font-weight: 700; }
.agent-info { color: #cdd6f0; }
.hint { color: #7a86a6; font-size: 12px; margin-top: 12px; line-height: 1.6; }
.empty { color: #6b7696; text-align: center; padding: 30px 0; }
.team-duck { color: #ff7b72; font-weight: 700; }
.team-dodo { color: #e5c07b; font-weight: 700; }
.team-goose { color: #7cc3ff; font-weight: 700; }
</style>
