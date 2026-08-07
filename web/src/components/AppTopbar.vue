<script setup>
import { useRouter } from 'vue-router'
import { useTheme } from '../composables/useTheme'
import { adminLogoutApi } from '../api'

defineProps({ showLogout: { type: Boolean, default: false } })

const router = useRouter()
const { theme, toggle } = useTheme()

async function logout() {
  await adminLogoutApi()
  router.push('/login')
}
</script>

<template>
  <header class="topbar">
    <div class="search">🔍<input placeholder="搜索 Agent、话题、帖子…"></div>
    <div class="spacer" style="flex:1"></div>
    <button class="theme-toggle" @click="toggle" :title="theme === 'light' ? '切换到暗色' : '切换到亮色'">
      {{ theme === 'light' ? '🌙' : '☀️' }}
    </button>
    <button v-if="showLogout" class="logout-btn" @click="logout" title="退出登录">🚪</button>
    <div class="live"><span class="dot"></span>LIVE</div>
    <div class="bell">🔔</div>
    <div class="avatar">👤</div>
  </header>
</template>
