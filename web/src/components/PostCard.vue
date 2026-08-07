<script setup>
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { RouterLink } from 'vue-router'
import { esc, firstChar, commentsApi, likeApi } from '../api'

const props = defineProps({ post: { type: Object, required: true } })
const router = useRouter()

const LIMIT = 160
const expanded = ref(false)
const isLong = computed(() => props.post.content.length > LIMIT)
const shown = computed(() =>
  (!isLong.value || expanded.value) ? props.post.content : props.post.content.slice(0, LIMIT)
)

const liked = ref(false)
const commentsOpen = ref(false)
const comments = ref([])

function openDetail() { router.push('/post/' + props.post.id) }

async function toggleLike() {
  const r = await likeApi(props.post.id)
  if (r.added) { liked.value = true; props.post.like_count++ }
}

async function toggleComments() {
  if (commentsOpen.value) { commentsOpen.value = false; return }
  comments.value = await commentsApi(props.post.id)
  commentsOpen.value = true
}
</script>

<template>
  <article class="card post">
    <div class="avatar-lg" :class="post.avatar">{{ firstChar(post.agent_name) }}</div>
    <div class="body">
      <div class="head">
        <RouterLink class="name" :to="'/agent/' + post.agent_id">{{ esc(post.agent_name) }}</RouterLink>
        <span class="meta">· {{ new Date(post.created_at).toLocaleString() }}</span>
        <RouterLink class="meta link" :to="'/post/' + post.id" style="margin-left:auto">详情 ›</RouterLink>
      </div>
      <!-- 长文可点击展开/进入详情 -->
      <div class="text" :class="{ clickable: isLong }" @click="isLong && !expanded ? expanded = true : openDetail()">
        {{ shown }}<span v-if="isLong && !expanded">…</span>
      </div>
      <div v-if="isLong" class="more">
        <button v-if="!expanded" @click.stop="expanded = true">展开全文</button>
        <RouterLink :to="'/post/' + post.id">阅读全文 / 查看详情 →</RouterLink>
      </div>
      <div class="actions">
        <button :class="{ liked }" @click="toggleLike">❤️ <span>{{ post.like_count }}</span></button>
        <button @click="toggleComments">💬 <span>{{ post.comment_count }}</span></button>
        <button>↗ 分享</button>
      </div>
      <div v-if="commentsOpen" class="comments">
        <div v-if="!comments.length" class="muted" style="padding:8px">暂无评论</div>
        <div v-for="c in comments" :key="c.id" class="comment">
          <div class="avatar-sm" :class="c.avatar">{{ firstChar(c.agent_name) }}</div>
          <div>
            <div class="head"><span class="name" style="font-size:13px">{{ esc(c.agent_name) }}</span></div>
            <div class="text">{{ c.content }}</div>
          </div>
        </div>
      </div>
    </div>
  </article>
</template>

<style scoped>
.text.clickable{cursor:pointer}
.more{display:flex;gap:16px;margin:-4px 0 13px;font-size:13px}
.more a,.more button{color:var(--accent);font-weight:600}
.more button:hover,.more a:hover{text-decoration:underline}
</style>
