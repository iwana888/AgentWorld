<script setup>
import { ref, onMounted } from 'vue'
import { analyticsApi } from '../api'

const data = ref(null)
const loading = ref(true)
const err = ref('')

async function load() {
  loading.value = true
  err.value = ''
  try {
    data.value = await analyticsApi()
  } catch (e) {
    err.value = '加载失败：' + e.message
  } finally {
    loading.value = false
  }
}

// 行为分布百分比
function distPct(action) {
  const d = data.value
  if (!d || !d.action_dist || !d.action_count) return 0
  return Math.round((d.action_dist[action] || 0) / d.action_count * 100)
}
function distCount(action) {
  return (data.value && data.value.action_dist && data.value.action_dist[action]) || 0
}

const actionMeta = [
  { key: 'post', label: '发帖', ico: '✍️', color: '#4c9be8' },
  { key: 'comment', label: '评论', ico: '💬', color: '#5ec48b' },
  { key: 'like', label: '点赞', ico: '👍', color: '#f0a35e' },
  { key: 'follow', label: '关注', ico: '👣', color: '#a78bfa' },
  { key: 'nothing', label: 'Nothing', ico: '😶', color: '#94a3b8' },
]

const relationMeta = [
  { key: 'friend', label: '好友', color: '#5ec48b' },
  { key: 'frequent_discuss', label: '频繁讨论', color: '#4c9be8' },
  { key: 'disagree', label: '观点对立', color: '#f0506e' },
  { key: 'block', label: '拉黑', color: '#94a3b8' },
]

function relCount(key) {
  const r = data.value && data.value.relation
  return (r && r[key]) || 0
}

onMounted(load)
</script>

<template>
  <div class="analytics">
    <div class="head">
      <h1>📊 世界数据分析</h1>
      <button class="refresh" @click="load">🔄 刷新</button>
    </div>

    <div v-if="loading" class="center">加载中…</div>
    <div v-else-if="err" class="center red">{{ err }}</div>
    <template v-else-if="data">
      <!-- 总览卡片 -->
      <div class="cards">
        <div class="card"><div class="num">{{ data.agent_count }}</div><div class="label">Agent</div></div>
        <div class="card"><div class="num">{{ data.post_count }}</div><div class="label">帖子</div></div>
        <div class="card"><div class="num">{{ data.comment_count }}</div><div class="label">评论</div></div>
        <div class="card"><div class="num">{{ data.like_count }}</div><div class="label">点赞</div></div>
        <div class="card"><div class="num">{{ data.follow_count }}</div><div class="label">关注</div></div>
        <div class="card"><div class="num">{{ data.memory_count }}</div><div class="label">记忆</div></div>
      </div>

      <div class="grid">
        <!-- 行为分布 -->
        <section class="panel">
          <h2>行为分布（共 {{ data.action_count }} 次）</h2>
          <div v-for="m in actionMeta" :key="m.key" class="bar-row">
            <span class="bar-label">{{ m.ico }} {{ m.label }}</span>
            <div class="bar-track">
              <div class="bar-fill" :style="{ width: distPct(m.key) + '%', background: m.color }"></div>
            </div>
            <span class="bar-val">{{ distCount(m.key) }}（{{ distPct(m.key) }}%）</span>
          </div>
        </section>

        <!-- 关系分布 -->
        <section class="panel">
          <h2>关系网络</h2>
          <div class="rel-list">
            <div v-for="m in relationMeta" :key="m.key" class="rel-item">
              <span class="rel-dot" :style="{ background: m.color }"></span>
              {{ m.label }}：<b>{{ relCount(m.key) }}</b>
            </div>
          </div>
          <div v-if="data.relation_net.length" class="rel-edges">
            <div v-for="(e, i) in data.relation_net" :key="i" class="edge">
              {{ e.from }} <span class="arrow">→</span> {{ e.to }} <em>({{ e.type }})</em>
            </div>
          </div>
          <div v-else class="dim">暂无关系，Agent 互动未达阈值或运行时间不足</div>
        </section>
      </div>

      <!-- 互动焦点 -->
      <section class="panel">
        <h2>🔥 互动焦点（讨论最集中）</h2>
        <div v-if="data.top_posts.length" class="hot-list">
          <div v-for="t in data.top_posts" :key="t.post_id" class="hot-item">
            <span class="hot-rank">#{{ t.post_id }}</span>
            <span class="hot-author">{{ t.agent_name }}</span>
            <span class="hot-content">{{ t.content }}</span>
            <span class="hot-stat">💬{{ t.comments }} 👍{{ t.likes }}</span>
          </div>
        </div>
        <div v-else class="dim">暂无互动</div>
      </section>

      <!-- Agent 画像 -->
      <section class="panel">
        <h2>Agent 画像</h2>
        <table class="agent-table">
          <thead>
            <tr><th>Agent</th><th>发帖</th><th>评论</th><th>点赞</th><th>关注</th><th>Nothing</th><th>记忆</th><th>LLM</th><th>目标</th></tr>
          </thead>
          <tbody>
            <tr v-for="a in data.agents" :key="a.id">
              <td class="name">{{ a.name }}<span v-if="a.kind==='human'" class="human-tag">人</span></td>
              <td>{{ a.posts }}</td><td>{{ a.comments }}</td><td>{{ a.likes }}</td>
              <td>{{ a.follows }}</td><td>{{ a.skips }}</td><td>{{ a.memories }}</td>
              <td><span class="llm-tag" :class="a.use_llm ? 'on' : 'off'">{{ a.use_llm ? 'LLM' : 'Mock' }}</span></td>
              <td class="goal">{{ a.goal }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </template>
  </div>
</template>

<style scoped>
.analytics { display: flex; flex-direction: column; gap: 16px; }
.head { display: flex; align-items: center; justify-content: space-between; }
.refresh { background: var(--accent); color: #fff; border: 0; border-radius: 8px; padding: 8px 16px; cursor: pointer; }
.center { text-align: center; padding: 40px; color: var(--text-dim); }
.center.red { color: var(--red); }

.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 12px; }
.card { background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px; padding: 16px; text-align: center; }
.card .num { font-size: 26px; font-weight: 700; color: var(--text); }
.card .label { font-size: 13px; color: var(--text-dim); margin-top: 4px; }

.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
@media (max-width: 900px) { .grid { grid-template-columns: 1fr; } }

.panel { background: var(--bg-card); border: 1px solid var(--border); border-radius: 12px; padding: 16px; }
.panel h2 { font-size: 15px; margin-bottom: 14px; color: var(--text); }

.bar-row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; font-size: 13px; }
.bar-label { width: 90px; flex-shrink: 0; color: var(--text); }
.bar-track { flex: 1; height: 10px; background: var(--bg-hover); border-radius: 5px; overflow: hidden; }
.bar-fill { height: 100%; border-radius: 5px; transition: width .3s; }
.bar-val { width: 110px; text-align: right; color: var(--text-dim); flex-shrink: 0; }

.rel-list { display: flex; flex-wrap: wrap; gap: 10px; margin-bottom: 12px; }
.rel-item { display: flex; align-items: center; gap: 6px; font-size: 13px; }
.rel-dot { width: 10px; height: 10px; border-radius: 50%; }
.rel-edges { max-height: 220px; overflow: auto; font-size: 13px; }
.edge { padding: 3px 0; color: var(--text); }
.edge .arrow { color: var(--accent); }
.edge em { color: var(--text-dim); font-style: normal; font-size: 12px; }

.hot-list { display: flex; flex-direction: column; gap: 6px; }
.hot-item { display: flex; align-items: center; gap: 10px; font-size: 13px; padding: 6px 8px; background: var(--bg-hover); border-radius: 8px; }
.hot-rank { color: var(--text-dim); font-size: 12px; flex-shrink: 0; }
.hot-author { color: var(--accent); font-weight: 600; flex-shrink: 0; }
.hot-content { flex: 1; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.hot-stat { color: var(--text-dim); font-size: 12px; flex-shrink: 0; }

.agent-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.agent-table th, .agent-table td { padding: 8px 10px; text-align: left; border-bottom: 1px solid var(--border); }
.agent-table th { color: var(--text-dim); font-weight: 600; font-size: 12px; }
.agent-table td { color: var(--text); }
.agent-table .name { font-weight: 600; white-space: nowrap; }
.human-tag { display: inline-block; margin-left: 4px; font-size: 11px; color: #fff; background: var(--accent); border-radius: 4px; padding: 0 4px; }
.llm-tag { font-size: 11px; border-radius: 4px; padding: 1px 6px; }
.llm-tag.on { background: rgba(92,196,139,.2); color: #5ec48b; }
.llm-tag.off { background: rgba(148,163,184,.2); color: #94a3b8; }
.agent-table .goal { color: var(--text-dim); max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dim { color: var(--text-faint); font-size: 13px; }
</style>
