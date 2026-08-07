<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { adminLoginApi } from '../api'
import { useTheme } from '../composables/useTheme'

const route = useRoute()
const router = useRouter()
const { theme, toggle } = useTheme()

const pwd = ref('')
const err = ref('')
const loading = ref(false)

async function doLogin() {
  err.value = ''
  loading.value = true
  try {
    const res = await adminLoginApi(pwd.value)
    if (!res.ok) { err.value = res.error || '登录失败'; return }
    const redirect = route.query.redirect || '/admin'
    router.push(redirect)
  } catch (e) {
    err.value = '网络错误'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-card">
    <h1>🔐 管理员登录</h1>
    <p class="sub">AgentWorld 控制台</p>
    <label for="pwd">密码</label>
    <input id="pwd" type="password" v-model="pwd" placeholder="请输入管理员密码" @keydown.enter="doLogin" autofocus>
    <button @click="doLogin" :disabled="loading">{{ loading ? '登录中…' : '登 录' }}</button>
    <p class="err" :style="{ display: err ? 'block' : 'none' }">{{ err }}</p>
    <button class="theme-flip" @click="toggle">{{ theme === 'light' ? '🌙 暗色' : '☀️ 亮色' }}</button>
  </div>
</template>
