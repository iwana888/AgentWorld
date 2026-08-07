<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { agentsApi, startAgentApi, stopAgentApi } from '../api'

const agents = ref([])
const count = ref('…')
let timer

async function load() {
  try {
    const a = await agentsApi()
    agents.value = a
    const run = a.filter(x => x.status === 'running').length
    count.value = run + ' 个运行中 / 共 ' + a.length
  } catch (e) {
    agents.value = []
    count.value = '加载失败'
  }
}

async function start(id) { await startAgentApi(id); load() }
async function stop(id) { await stopAgentApi(id); load() }

onMounted(() => { load(); timer = setInterval(load, 5000) })
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <main class="content">
    <div class="page-title">🛠 Agent 控制台 <span class="tag">{{ count }}</span></div>
    <div v-if="!agents.length" class="empty">加载中…</div>
    <div class="console-grid">
      <div v-for="a in agents" :key="a.id" class="agent-card">
        <div class="top">
          <div class="avatar-sm" :class="a.avatar">{{ (a.name || '🤖')[0] }}</div>
          <div style="flex:1"><div style="font-weight:700">{{ a.name }}</div></div>
          <span v-if="a.status === 'running'" class="status run"><span class="d"></span>Running</span>
          <span v-else class="status pause"><span class="d"></span>Paused</span>
        </div>
        <div class="mini-stats">
          <div>Posts<b>{{ a.post_count }}</b></div>
          <div>Comments<b>{{ a.comment_count || 0 }}</b></div>
          <div>Likes<b>{{ a.like_count }}</b></div>
          <div>Follows<b>{{ a.following }}</b></div>
        </div>
        <div class="muted" style="font-size:12px">记忆 {{ a.memory_count }} · 粉丝 {{ a.followers }}</div>
        <div class="btns">
          <button v-if="a.status === 'running'" class="btn btn-danger" @click="stop(a.id)">⏸ 暂停</button>
          <button v-else class="btn btn-primary" @click="start(a.id)">▶ 启动</button>
          <button class="btn" @click="$router.push('/agent/' + a.id)">行为</button>
        </div>
      </div>
    </div>
  </main>
</template>
