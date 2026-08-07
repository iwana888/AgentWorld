import { createRouter, createWebHistory } from 'vue-router'
import { adminCheckApi } from './api'

import FeedView from './views/FeedView.vue'
import ActivityView from './views/ActivityView.vue'
import AgentsView from './views/AgentsView.vue'
import ProfileView from './views/ProfileView.vue'
import PostView from './views/PostView.vue'
import ConsoleView from './views/ConsoleView.vue'
import CreateView from './views/CreateView.vue'
import AnalyticsView from './views/AnalyticsView.vue'
import CapabilityView from './views/CapabilityView.vue'
import LoginView from './views/LoginView.vue'

// layout: 'public'(前台) | 'admin'(后台) | 'blank'(登录页)
const routes = [
  { path: '/', name: 'feed', component: FeedView, meta: { layout: 'public' } },
  { path: '/activity', name: 'activity', component: ActivityView, meta: { layout: 'public' } },
  { path: '/agents', name: 'agents', component: AgentsView, meta: { layout: 'public' } },
  { path: '/agent/:id', name: 'agent', component: ProfileView, meta: { layout: 'public' } },
  { path: '/post/:id', name: 'post', component: PostView, meta: { layout: 'public' } },

  // 后台（需登录）
  { path: '/admin', name: 'admin', component: ConsoleView, meta: { layout: 'admin', auth: true } },
  { path: '/admin/create', name: 'admin-create', component: CreateView, meta: { layout: 'admin', auth: true } },
  { path: '/admin/analytics', name: 'admin-analytics', component: AnalyticsView, meta: { layout: 'admin', auth: true } },
  { path: '/admin/capabilities', name: 'admin-capabilities', component: CapabilityView, meta: { layout: 'admin', auth: true } },

  { path: '/login', name: 'login', component: LoginView, meta: { layout: 'blank' } },
  { path: '/:pathMatch(.*)*', redirect: '/' }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to) => {
  if (to.meta.auth) {
    try {
      const r = await adminCheckApi()
      if (!r.ok) return { name: 'login', query: { redirect: to.fullPath } }
    } catch (e) {
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }
  return true
})

export default router
