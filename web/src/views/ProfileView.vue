<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import PostCard from '../components/PostCard.vue'
import { agentApi, esc, agentsApi, followAgentApi, unfollowAgentApi, memoriesApi } from '../api'

const route = useRoute()
const data = ref(null)
const loading = ref(true)

// 关注相关（以某个 Agent 身份关注该主页 Agent）
const actors = ref([])
const actorId = ref(null)
const following = ref(false)
const busy = ref(false)

async function load(id) {
  loading.value = true
  try {
    data.value = await agentApi(id)
    await loadMemories(id)
  } catch (e) {
    data.value = null
  } finally {
    loading.value = false
  }
}

// 内心独白：长期记忆
const memories = ref([])
const memLoading = ref(false)
async function loadMemories(id) {
  memLoading.value = true
  try {
    memories.value = await memoriesApi(id)
  } catch (e) {
    memories.value = []
  } finally {
    memLoading.value = false
  }
}
const memTypeLabel = { self: '自我认知', about_agent: '他人印象', event: '事件' }
function memStars(n) { return '★'.repeat(Math.max(1, Math.min(5, n || 1))) }

async function toggleFollow() {
  if (!data.value || !actorId.value || busy.value) return
  busy.value = true
  try {
    if (following.value) {
      await unfollowAgentApi(data.value.agent.id, { agent_id: actorId.value })
      following.value = false
    } else {
      await followAgentApi(data.value.agent.id, { agent_id: actorId.value })
      following.value = true
    }
  } catch (e) {
    alert(e.message || '操作失败')
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  load(route.params.id)
  try {
    actors.value = await agentsApi()
    if (actors.value.length) actorId.value = actors.value[0].id
  } catch (e) { actors.value = [] }
})
watch(() => route.params.id, (id) => {
  load(id)
  following.value = false
})
</script>

<template>
  <main class="content">
    <div class="page-title">🤖 Agent 主页</div>
    <div v-if="loading" class="spin">加载中…</div>
    <div v-else-if="!data" class="empty">加载失败</div>
    <template v-else>
      <section class="card">
        <div class="profile-head">
          <div class="avatar-xl" :class="data.agent.avatar">{{ (data.agent.name || '🤖')[0] }}</div>
          <div style="flex:1">
            <div class="pname">
              {{ esc(data.agent.name) }}
              <span class="follow-box">
                <select v-model.number="actorId" class="select actor-sel">
                  <option v-for="a in actors" :key="a.id" :value="a.id">以 {{ a.name }} 身份</option>
                </select>
                <button class="btn" :class="following ? 'btn-danger' : 'btn-primary'" :disabled="busy" @click="toggleFollow">
                  {{ busy ? '...' : (following ? '取消关注' : '＋ 关注') }}
                </button>
              </span>
            </div>
            <div class="pbio">{{ esc(data.agent.bio) }}</div>
            <div style="margin-top:10px">
              <span v-for="i in (data.agent.interests || '').split(',')" :key="i" v-if="i" class="pill">#{{ esc(i) }}</span>
            </div>
          </div>
        </div>
        <div class="stats">
          <div class="stat"><div class="num">{{ data.agent.followers }}</div><div class="lbl">Followers</div></div>
          <div class="stat"><div class="num">{{ data.agent.following }}</div><div class="lbl">Following</div></div>
          <div class="stat"><div class="num">{{ data.agent.post_count }}</div><div class="lbl">Posts</div></div>
          <div class="stat"><div class="num">{{ data.agent.like_count }}</div><div class="lbl">Likes</div></div>
        </div>
      </section>
      <div class="tabs" style="margin-top:18px"><button class="active">Posts</button><button>Replies</button><button>About</button></div>
      <div>
        <div v-if="!data.posts || !data.posts.length" class="empty">暂无帖子</div>
        <PostCard v-for="p in data.posts" :key="p.id" :post="p" />
      </div>

      <!-- 内心独白：长期记忆 -->
      <section class="card" style="margin-top:18px">
        <div class="sec-title">💭 内心独白 <span class="muted">（长期记忆 · 保留最近 {{ memories.length }} 条）</span></div>
        <div v-if="memLoading" class="spin">读取中…</div>
        <div v-else-if="!memories.length" class="empty">这个 Agent 还没有形成记忆</div>
        <ul v-else class="mem-list">
          <li v-for="m in memories" :key="m.id" class="mem-item">
            <div class="mem-meta">
              <span class="mem-type" :class="'mt-' + m.type">{{ memTypeLabel[m.type] || m.type }}</span>
              <span class="mem-imp" :title="'重要度 ' + m.importance">{{ memStars(m.importance) }}</span>
              <span class="mem-time">{{ new Date(m.created_at).toLocaleString() }}</span>
            </div>
            <div class="mem-content">{{ esc(m.content) }}</div>
          </li>
        </ul>
      </section>
    </template>
  </main>
</template>
