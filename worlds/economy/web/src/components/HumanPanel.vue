<template>
  <el-card class="hp" shadow="never">
    <template #header>
      <div class="hp-title">👤 人类接入 <span class="hp-sub">M7 Human Entrance</span></div>
    </template>

    <!-- 未登录：注册 / 登录 -->
    <div v-if="!me.id" class="hp-auth">
      <el-input v-model="authName" placeholder="用户名" size="small" class="hp-input" />
      <el-input v-model="authPass" placeholder="密码" type="password" size="small" class="hp-input" show-password />
      <div class="hp-btn-row">
        <el-button type="primary" size="small" @click="register">注册</el-button>
        <el-button size="small" @click="login">登录</el-button>
      </div>
      <div class="hp-tip">作为 Human Agent 进入经济世界，可工作 / 买技能 / 雇 AI</div>
    </div>

    <!-- 已登录：我的 Agent -->
    <div v-else class="hp-me">
      <div class="hp-id">
        <span class="hp-name">{{ me.name }}</span>
        <span class="hp-kind">HUMAN</span>
      </div>
      <div class="hp-stats">
        <div class="hp-cell">
          <div class="hp-label">余额</div>
          <div class="hp-val gold">{{ me.balance }}</div>
        </div>
        <div class="hp-cell">
          <div class="hp-label">声誉</div>
          <div class="hp-val" :class="repClass(me.reputation)">♛{{ me.reputation }}</div>
        </div>
        <div class="hp-cell">
          <div class="hp-label">完成/失败</div>
          <div class="hp-val">{{ me.completed }}/{{ me.failed }}</div>
        </div>
      </div>
      <div class="hp-skills">
        <span v-for="sk in me.skills" :key="sk.skillID" class="hp-skill">
          {{ sk.skillID }} Lv{{ sk.level }}
        </span>
      </div>

      <!-- 行动入口 -->
      <div class="hp-actions">
        <div class="hp-act">
          <el-select v-model="jobSel" placeholder="选一份工作" size="small" class="hp-sel">
            <el-option v-for="j in snapshot.openJobs" :key="j.id" :value="j.id" :label="`${j.title} +${j.reward} (${j.skill})`" />
          </el-select>
          <el-button size="small" type="success" @click="doJob">工作</el-button>
        </div>
        <div class="hp-act">
          <el-select v-model="skillSel" placeholder="买技能" size="small" class="hp-sel">
            <el-option v-for="s in snapshot.skillMarket" :key="s.skillID" :value="s.skillID" :label="`${s.name} ${s.price}`" />
          </el-select>
          <el-button size="small" type="warning" @click="buySkill">购买</el-button>
        </div>
        <div class="hp-act">
          <el-select v-model="hireSel" placeholder="雇 AI 做服务" size="small" class="hp-sel">
            <el-option v-for="s in snapshot.services" :key="s.id" :value="s" :label="`${s.name} (${s.availableWorkers}人)`" />
          </el-select>
          <el-button size="small" type="danger" @click="hireAgent">雇佣</el-button>
        </div>
      </div>

      <el-button size="small" text class="hp-logout" @click="logout">退出登录</el-button>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import type { GameSnapshot } from '../types'

const props = defineProps<{ snapshot: GameSnapshot }>()

const me = reactive<{
  id: number; name: string; balance: number; reputation: number
  skills: { skillID: string; level: number }[]; completed: number; failed: number
}>({ id: 0, name: '', balance: 0, reputation: 0, skills: [], completed: 0, failed: 0 })

const authName = ref('')
const authPass = ref('')
const jobSel = ref<number>(0)
const skillSel = ref('')
const hireSel = ref<any>(null)

let token = localStorage.getItem('eco_human_token') || ''

onMounted(async () => {
  if (token) await refreshMe()
})

// 通过 /api/world 拉取"我的 Agent"
async function refreshMe() {
  try {
    const res = await fetch('api/world', { headers: { Authorization: 'Bearer ' + token } })
    const data = await res.json()
    if (data.me && data.me.id) applyMe(data.me)
  } catch { /* ignore */ }
}
function applyMe(m: any) {
  me.id = m.id; me.name = m.name; me.balance = m.balance; me.reputation = m.reputation
  me.skills = m.skills || []; me.completed = m.completed; me.failed = m.failed
}

async function register() {
  const res = await fetch('api/auth/register', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: authName.value, password: authPass.value }),
  })
  const data = await res.json()
  if (data.token) { token = data.token; localStorage.setItem('eco_human_token', token); ElMessage.success('注册成功'); await refreshMe() }
  else ElMessage.error(data.error || '注册失败')
}
async function login() {
  const res = await fetch('api/auth/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: authName.value, password: authPass.value }),
  })
  const data = await res.json()
  if (data.token) { token = data.token; localStorage.setItem('eco_human_token', token); ElMessage.success('登录成功'); await refreshMe() }
  else ElMessage.error('登录失败')
}
function logout() { token = ''; localStorage.removeItem('eco_human_token'); me.id = 0 }

async function doJob() {
  if (!jobSel.value) return ElMessage.warning('先选工作')
  const res = await fetch('api/actions/do_job', {
    method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ job_id: jobSel.value }),
  })
  const d = await res.json()
  ElMessage[d.success ? 'success' : 'error'](d.message)
  await refreshMe()
}
async function buySkill() {
  if (!skillSel.value) return ElMessage.warning('先选技能')
  const res = await fetch('api/actions/buy_skill', {
    method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ skill_id: skillSel.value }),
  })
  const d = await res.json()
  ElMessage[d.success ? 'success' : 'error'](d.message)
  await refreshMe()
}
async function hireAgent() {
  if (!hireSel.value) return ElMessage.warning('先选服务')
  // 选该服务的第一个可用 worker（演示：前端简化，MVP 直接雇第一个）
  const worker = hireSel.value.workers?.[0]?.agentID
  if (!worker) return ElMessage.warning('该服务暂无 worker')
  const res = await fetch('api/actions/hire_agent', {
    method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: JSON.stringify({ service_id: hireSel.value.id, worker_id: worker }),
  })
  const d = await res.json()
  ElMessage[d.success ? 'success' : 'error'](d.message)
  await refreshMe()
}

function repClass(r: number) { return r >= 80 ? 'good' : r >= 50 ? 'mid' : 'bad' }
</script>

<style scoped>
.hp { background: #121826; border: 1px solid #2a3550; }
.hp-title { color: #e2e9ff; font-weight: 800; }
.hp-sub { font-size: 11px; color: #6b7696; font-weight: 400; margin-left: 6px; }
.hp-auth { display: flex; flex-direction: column; gap: 6px; }
.hp-input { width: 100%; }
.hp-btn-row { display: flex; gap: 6px; }
.hp-tip { font-size: 10px; color: #7a86a6; }
.hp-id { display: flex; align-items: center; gap: 6px; margin-bottom: 6px; }
.hp-name { font-size: 15px; font-weight: 800; color: #e2e9ff; }
.hp-kind { font-size: 9px; color: #7cc3ff; background: #0f2a3f; padding: 1px 6px; border-radius: 8px; }
.hp-stats { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 4px; margin-bottom: 6px; }
.hp-cell { background: #10162a; border: 1px solid #232f4a; border-radius: 6px; padding: 5px; text-align: center; }
.hp-label { font-size: 9px; color: #7a86a6; }
.hp-val { font-size: 13px; font-weight: 800; }
.hp-val.gold { color: #ffd166; }
.hp-val.good { color: #9ee6b0; }
.hp-val.mid { color: #e5c07b; }
.hp-val.bad { color: #ff7b72; }
.hp-skills { display: flex; flex-wrap: wrap; gap: 3px; margin-bottom: 8px; }
.hp-skill { font-size: 10px; color: #7cc3ff; background: #0f2a3f; padding: 1px 6px; border-radius: 8px; }
.hp-actions { display: flex; flex-direction: column; gap: 5px; }
.hp-act { display: flex; gap: 5px; align-items: center; }
.hp-sel { flex: 1; }
.hp-logout { margin-top: 6px; color: #7a86a6; }
</style>
