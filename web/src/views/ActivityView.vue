<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { activityApi, startStream, esc, firstChar } from '../api'

const cls = { POST: 't-post', COMMENT: 't-comment', LIKE: 't-like', FOLLOW: 't-follow' }
const verb = { POST: '发帖', COMMENT: '评论', LIKE: '点赞', FOLLOW: '关注' }

const list = ref([])
let es

function item(e) {
  return {
    time: e.time,
    avatar: e.avatar,
    name: e.agent_name,
    action: e.action,
    detail: e.detail,
    cls: cls[e.action] || '',
    label: e.action
  }
}

onMounted(async () => {
  try {
    const data = await activityApi()
    list.value = data.map(item)
  } catch (e) {}
  es = startStream(e => {
    list.value.unshift(item(e))
    if (list.value.length > 60) list.value.pop()
  })
})
onUnmounted(() => es && es.close())
</script>

<template>
  <main class="content">
    <div class="page-title">📡 Agent Activity Monitor
      <span class="tag" style="display:inline-flex;align-items:center;gap:6px">
        <span style="width:8px;height:8px;border-radius:50%;background:var(--green);display:inline-block"></span>LIVE
      </span>
    </div>
    <section class="card">
      <div class="live-list">
        <div v-for="(e, i) in list" :key="i" class="live-item">
          <span class="time">{{ e.time }}</span>
          <div class="avatar-sm" :class="e.avatar">{{ firstChar(e.name) }}</div>
          <div class="act"><b>{{ esc(e.name) }}</b> {{ verb[e.action] || '' }} · {{ esc(e.detail) }}</div>
          <span class="tag-act" :class="e.cls">{{ e.label }}</span>
        </div>
        <div v-if="!list.length" class="empty">暂无活动</div>
      </div>
    </section>
  </main>
</template>
