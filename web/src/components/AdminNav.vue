<script setup>
import { RouterLink, useRouter } from 'vue-router'
import { adminLogoutApi } from '../api'

const router = useRouter()

const links = [
  { to: '/admin', ico: '🛠', label: '控制台' },
  { to: '/admin/capabilities', ico: '🧩', label: '能力实验室' },
  { to: '/admin/analytics', ico: '📊', label: '数据分析' },
  { to: '/admin/create', ico: '➕', label: '创建 Agent' },
]

async function logout() {
  await adminLogoutApi()
  router.push('/login')
}
</script>

<template>
  <aside class="sidebar">
    <div class="brand">
      <div class="logo">🛡</div>
      <div>AgentWorld<span class="sub">管理后台</span></div>
    </div>
    <nav class="nav">
      <RouterLink v-for="l in links" :key="l.to" :to="l.to" class="nav-link">
        <span class="ico">{{ l.ico }}</span>{{ l.label }}
      </RouterLink>
      <div class="spacer"></div>
      <RouterLink to="/" class="nav-back">← 返回前台</RouterLink>
      <button class="nav-logout" @click="logout">🚪 退出登录</button>
    </nav>
  </aside>
</template>

<style scoped>
.nav-link{display:flex;align-items:center;gap:12px;padding:11px 13px;border-radius:10px;color:var(--text-dim);font-weight:500;transition:.15s;font-size:15px}
.nav-link:hover{background:var(--bg-hover);color:var(--text)}
.nav-link.router-link-exact-active{background:var(--bg-hover);color:var(--text);box-shadow:inset 3px 0 0 var(--accent)}
.nav-back{display:flex;align-items:center;gap:12px;padding:11px 13px;border-radius:10px;color:var(--text-faint);font-size:14px;transition:.15s}
.nav-back:hover{background:var(--bg-hover);color:var(--text)}
.nav-logout{display:flex;align-items:center;gap:12px;padding:11px 13px;border-radius:10px;color:var(--red);font-weight:500;font-size:14px;transition:.15s}
.nav-logout:hover{background:rgba(248,81,73,.14)}
</style>
