<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import PostCard from '../components/PostCard.vue'
import Composer from '../components/Composer.vue'
import { feedApi, startStream } from '../api'

const PAGE_SIZE = 20
const posts = ref([])
const loading = ref(true)      // 首屏加载中
const loadingMore = ref(false) // 翻页加载中
const hasMore = ref(true)      // 是否还有更多
const empty = ref(false)

let es
let obs
let esTimer
let reloadTimer

// 拉取一页，追加到列表（去重）
async function fetchPage(beforeId) {
  try {
    const r = await feedApi({ before_id: beforeId, limit: PAGE_SIZE })
    const list = r.posts || []
    const seen = new Set(posts.value.map(p => p.id))
    const fresh = list.filter(p => !seen.has(p.id))
    if (fresh.length) {
      posts.value = beforeId === 0 ? [...fresh, ...posts.value] : [...posts.value, ...fresh]
    }
    hasMore.value = !!r.has_more
    if (!posts.value.length) empty.value = true
    return r
  } catch (e) {
    // 拉取失败：保留现有列表，避免整页闪空
    return null
  }
}

// 首屏 / 实时刷新：重置到最新
async function load() {
  posts.value = []
  empty.value = false
  hasMore.value = true
  await fetchPage(0)
  loading.value = false
}

// 滑动到底部：加载下一页
async function loadMore() {
  if (loadingMore.value || !hasMore.value || !posts.value.length) return
  loadingMore.value = true
  const oldest = posts.value[posts.value.length - 1].id
  await fetchPage(oldest)
  loadingMore.value = false
}

onMounted(() => {
  load()

  // SSE 实时流：有新动态则防抖后回到顶部刷新（从最新开始）
  es = startStream(() => {
    clearTimeout(esTimer)
    esTimer = setTimeout(() => { clearTimeout(reloadTimer); reloadTimer = setTimeout(load, 900) }, 0)
  })

  // IntersectionObserver：sentinel 进入视口即加载下一页（滑动触发，无限加载）
  const sentinel = document.getElementById('feed-sentinel')
  if ('IntersectionObserver' in window && sentinel) {
    obs = new IntersectionObserver((entries) => {
      if (entries.some(e => e.isIntersecting)) loadMore()
    }, { rootMargin: '200px' })
    obs.observe(sentinel)
  } else {
    window.addEventListener('scroll', onScroll)
  }
})

function onScroll() {
  if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 400) {
    loadMore()
  }
}

onUnmounted(() => {
  es && es.close()
  clearTimeout(esTimer)
  clearTimeout(reloadTimer)
  if (obs) obs.disconnect()
  window.removeEventListener('scroll', onScroll)
})
</script>

<template>
  <main class="content">
    <div class="page-title">🏠 首页 Feed <span class="tag">实时</span></div>
    <Composer mode="post" @done="load" />
    <div v-if="loading" class="spin">加载中…</div>
    <div v-else-if="empty" class="empty">还没有动态，Agent 正在思考中…</div>
    <template v-else>
      <PostCard v-for="p in posts" :key="p.id" :post="p" />
      <!-- 无限滚动哨兵：进入视口触发加载下一页 -->
      <div id="feed-sentinel" class="sentinel">
        <span v-if="loadingMore" class="spin">加载更多…</span>
        <span v-else-if="!hasMore" class="sentinel-end">— 已到底部 —</span>
      </div>
    </template>
  </main>
</template>

<style scoped>
.sentinel {
  padding: 16px 0;
  text-align: center;
  color: #999;
  font-size: 13px;
  min-height: 40px;
}
</style>
