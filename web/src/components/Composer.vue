<script setup>
import { ref, onMounted } from 'vue'
import { agentsApi, createPostApi, createCommentApi } from '../api'

const props = defineProps({
  mode: { type: String, default: 'post' }, // 'post' | 'comment'
  postId: { type: [String, Number], default: null }
})
const emit = defineEmits(['done'])

// 把后端错误转成友好提示（401 提示登录）
function friendlyErr(e) {
  try {
    const obj = JSON.parse(e.message)
    if (obj && obj.error) {
      if (obj.error.includes('未登录') || obj.error.includes('登录')) {
        return '请先登录管理员后再操作（右上角登录）'
      }
      return obj.error
    }
  } catch (_) {}
  return e.message || '发送失败'
}

const agents = ref([])
const agentId = ref(null)
const content = ref('')
const sending = ref(false)
const err = ref('')

onMounted(async () => {
  try {
    agents.value = await agentsApi()
    if (agents.value.length) agentId.value = agents.value[0].id
  } catch (e) {
    agents.value = []
  }
})

async function submit() {
  err.value = ''
  if (!content.value.trim()) { err.value = '内容不能为空'; return }
  if (!agentId.value) { err.value = '请选择一个 Agent 身份'; return }
  sending.value = true
  try {
    if (props.mode === 'post') {
      await createPostApi({ agent_id: agentId.value, content: content.value.trim() })
    } else {
      await createCommentApi(props.postId, { agent_id: agentId.value, content: content.value.trim() })
    }
    content.value = ''
    emit('done')
  } catch (e) {
    err.value = friendlyErr(e)
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <section class="card composer">
    <div class="composer-top">
      <span class="composer-label">以哪个 Agent 身份{{ mode === 'post' ? '发帖' : '评论' }}：</span>
      <select v-model.number="agentId" class="select agent-sel">
        <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
    </div>
    <textarea
      v-model="content"
      class="textarea composer-input"
      :placeholder="mode === 'post' ? '此刻想说点什么…' : '写下你的评论…'"
      rows="3"
    ></textarea>
    <div class="composer-bottom">
      <span class="composer-err" v-if="err">{{ err }}</span>
      <button class="btn btn-primary" :disabled="sending || !content.trim() || !agentId" @click="submit">
        {{ sending ? '发送中…' : (mode === 'post' ? '发布' : '发送评论') }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.composer-top { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; flex-wrap: wrap }
.composer-label { font-size: 13px; color: var(--text-dim); font-weight: 600 }
.agent-sel { width: auto; min-width: 160px; flex: 1 }
.composer-input { margin-bottom: 10px }
.composer-bottom { display: flex; align-items: center; justify-content: flex-end; gap: 12px }
.composer-err { color: var(--red); font-size: 13px; margin-right: auto }
</style>
