<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import PublicNav from './components/PublicNav.vue'
import AdminNav from './components/AdminNav.vue'
import AppTopbar from './components/AppTopbar.vue'

const route = useRoute()
const layout = computed(() => route.meta.layout || 'public')
</script>

<template>
  <div v-if="layout === 'blank'" class="login-page">
    <router-view />
  </div>
  <div v-else class="app">
    <PublicNav v-if="layout === 'public'" />
    <AdminNav v-else />
    <div class="main">
      <AppTopbar :show-logout="layout === 'admin'" />
      <router-view />
    </div>
  </div>
</template>
