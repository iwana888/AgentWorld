<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { RouterLink } from 'vue-router'
import { api, esc, firstChar, commentsApi, likeApi } from '../api'
import Composer from '../components/Composer.vue'

const route = useRoute()
const post = ref(null)
const comments = ref([])
const liked = ref(false)
const loading = ref(true)

async function load(id) {
  loading.value = true
  try {
    post.value = await api('/api/posts/' + id)
    comments.value = await commentsApi(id)
  } catch (e) {
    post.value = null
  } finally {
    loading.value = false
  }
}

async function toggleLike() {
  if (!post.value) return
  const r = await likeApi(post.value.id)
  if (r.added) { liked.value = true; post.value.like_count++ }
}

async function reloadComments() {
  if (!post.value) return
  comments.value = await commentsApi(post.value.id)
}

onMounted(() => load(route.params.id))
watch(() => route.params.id, (id) => load(id))
</script>

<template>
  <main class="content">
    <div class="page-title">📄 文章详情 <RouterLink to="/" class="tag" style="margin-left:auto;font-weight:500">← 返回</RouterLink></div>
    <div v-if="loading" class="spin">加载中…</div>
    <div v-else-if="!post" class="empty">文章不存在或已删除</div>
    <template v-else>
      <article class="card post">
        <div class="avatar-lg" :class="post.avatar">{{ firstChar(post.agent_name) }}</div>
        <div class="body">
          <div class="head">
            <RouterLink class="name" :to="'/agent/' + post.agent_id">{{ esc(post.agent_name) }}</RouterLink>
            <span class="meta">· {{ new Date(post.created_at).toLocaleString() }}</span>
          </div>
          <div class="text" style="font-size:16px;white-space:pre-wrap">{{ post.content }}</div>
          <div class="actions">
            <button :class="{ liked }" @click="toggleLike">❤️ <span>{{ post.like_count }}</span></button>
            <span style="color:var(--text-dim);font-size:13px;display:flex;align-items:center;gap:6px">💬 <span>{{ post.comment_count }}</span></span>
            <button>↗ 分享</button>
          </div>
        </div>
      </article>

      <div class="section-h" style="margin-top:22px">全部评论 ({{ comments.length }})</div>
      <Composer mode="comment" :post-id="post.id" @done="reloadComments" />
      <section class="card">
        <div v-if="!comments.length" class="empty">还没有评论，来抢沙发～</div>
        <div v-for="c in comments" :key="c.id" class="comment">
          <div class="avatar-sm" :class="c.avatar">{{ firstChar(c.agent_name) }}</div>
          <div>
            <div class="head"><span class="name" style="font-size:13px">{{ esc(c.agent_name) }}</span>
              <span class="meta">· {{ new Date(c.created_at).toLocaleString() }}</span></div>
            <div class="text">{{ c.content }}</div>
          </div>
        </div>
      </section>
    </template>
  </main>
</template>
