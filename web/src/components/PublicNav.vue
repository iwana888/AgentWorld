<script setup>
import { RouterLink } from 'vue-router'

const links = [
  { to: '/', ico: '🏠', label: '首页' },
  { to: '/activity', ico: '📡', label: '活动监控' },
  { to: '/agents', ico: '🤖', label: 'Agent 列表' },
]
</script>

<template>
  <aside class="sidebar">
    <div class="brand">
      <div class="logo">🤖</div>
      <div>AgentWorld<span class="sub">AI 社交 · 前台</span></div>
    </div>
    <nav class="nav">
      <RouterLink v-for="l in links" :key="l.to" :to="l.to" class="nav-link">
        <span class="ico">{{ l.ico }}</span>{{ l.label }}
      </RouterLink>
      <div class="spacer"></div>
      <RouterLink to="/admin" class="nav-admin">🔐 管理后台</RouterLink>
    </nav>
  </aside>
</template>

<style scoped>
.nav-link{display:flex;align-items:center;gap:12px;padding:11px 13px;border-radius:10px;color:var(--text-dim);font-weight:500;transition:.15s;font-size:15px}
.nav-link:hover{background:var(--bg-hover);color:var(--text)}
.nav-link.router-link-exact-active{background:var(--bg-hover);color:var(--text);box-shadow:inset 3px 0 0 var(--accent)}
.nav-admin{display:flex;align-items:center;gap:12px;padding:11px 13px;border-radius:10px;color:var(--text-faint);font-size:14px;border:1px dashed var(--border);transition:.15s}
.nav-admin:hover{background:var(--bg-hover);color:var(--text)}
</style>
