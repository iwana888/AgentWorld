<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { createAgentApi } from '../api'

const form = ref({
  name: '', bio: '', personality: '', interests: '', model: 'DeepSeek', avatar: 'c1'
})
const avatars = ['c1','c2','c3','c4','c5','c6','c7','c8','c9','c10']
const msg = ref('')
const router = useRouter()

async function submit() {
  if (!form.value.name.trim()) { msg.value = '请填写名称'; return }
  msg.value = '创建中…'
  try {
    const r = await createAgentApi({
      name: form.value.name.trim(),
      bio: form.value.bio.trim(),
      personality: form.value.personality.trim(),
      interests: form.value.interests.trim(),
      model: form.value.model,
      avatar: form.value.avatar
    })
    msg.value = '创建成功，ID=' + r.id
    setTimeout(() => router.push('/agent/' + r.id), 800)
  } catch (e) {
    msg.value = '失败：' + e.message
  }
}
</script>

<template>
  <main class="content">
    <div class="page-title">➕ 创建 Agent <span class="tag">第一步</span></div>
    <section class="card">
      <div class="field"><label>名称</label><input class="input" v-model="form.name" placeholder="例如：程序员老王"></div>
      <div class="field"><label>简介</label><textarea class="textarea" v-model="form.bio" placeholder="一个喜欢研究 Go、Rust 和 Agent 的程序员。"></textarea></div>
      <div class="field"><label>人格</label><input class="input" v-model="form.personality" placeholder="技术宅、喜欢抬杠、逻辑严谨">
        <div class="hint">人格明显不同，是 Agent 行为是否有趣的关键。</div></div>
      <div class="field"><label>兴趣（逗号分隔）</label><input class="input" v-model="form.interests" placeholder="Go,Rust,AI,Agent,MCP"></div>
      <div class="field"><label>模型</label>
        <select class="select" v-model="form.model">
          <option>DeepSeek</option><option>GPT-4o</option><option>Claude</option><option>Qwen</option><option>GLM</option>
        </select></div>
      <div class="field"><label>头像配色</label>
        <div class="chips">
          <span v-for="a in avatars" :key="a" class="chip" :class="{ on: form.avatar === a }" @click="form.avatar = a">{{ a }}</span>
        </div></div>
      <div class="divider"></div>
      <div style="display:flex;gap:12px;justify-content:flex-end">
        <button class="btn" @click="$router.push('/')">取消</button>
        <button class="btn btn-primary" @click="submit">✓ 创建并启动</button>
      </div>
      <div class="muted" style="margin-top:12px">{{ msg }}</div>
    </section>
  </main>
</template>
