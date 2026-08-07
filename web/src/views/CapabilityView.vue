<script setup>
// M9 Capability System · 能力实验室（admin 后台，需登录）
// 借鉴 test-agent 的"工具调用卡片 + 折叠交互"，展示并调用已注册能力（含 RAG 知识检索）。
import { ref, reactive, onMounted, computed } from 'vue'
import { capabilitiesApi, callCapabilityApi } from '../api'

const caps = ref([])          // 能力列表
const loading = ref(true)
const errMsg = ref('')
const busy = ref(false)       // 正在调用中
const activeTool = ref(null)  // 当前展开的工具 {capName, tool}

// 参数表单值：key = capName + '/' + toolName
const formValues = reactive({})
// 调用历史（本页会话内）
const logs = ref([])

async function load() {
  loading.value = true
  errMsg.value = ''
  try {
    caps.value = await capabilitiesApi()
  } catch (e) {
    errMsg.value = '加载能力失败：' + e.message
    caps.value = []
  } finally {
    loading.value = false
  }
}

// 能力下所有工具的扁平视图（带能力名）
const allTools = computed(() => {
  const out = []
  for (const c of caps.value) {
    for (const t of (c.tools || [])) out.push({ capName: c.name, capDesc: c.desc, ...t })
  }
  return out
})

const toolCount = computed(() => allTools.value.length)

function formKey(capName, toolName) { return capName + '/' + toolName }

function toggle(capName, tool) {
  const key = formKey(capName, tool.name)
  activeTool.value = (activeTool.value && activeTool.value.key === key) ? null : { key, capName, tool }
  if (!formValues[key]) {
    const v = {}
    for (const p of (tool.parameters || [])) {
      v[p.name] = p.default !== undefined && p.default !== null ? String(p.default) : ''
    }
    formValues[key] = v
  }
}

// 类型提示/默认值
function typeHint(p) {
  switch (p.type) {
    case 'number': case 'integer': return 'number'
    case 'boolean': return 'true / false'
    default: return 'text'
  }
}

// 转换参数：按 schema 类型把字符串转成 number/boolean
function coerceArgs(parameters, raw) {
  const out = {}
  for (const p of (parameters || [])) {
    let v = raw[p.name]
    if (v === undefined || v === null || v === '') continue
    if (p.type === 'number' || p.type === 'integer') {
      const n = Number(v)
      if (!isNaN(n)) out[p.name] = n
      else out[p.name] = v
    } else if (p.type === 'boolean') {
      out[p.name] = v === 'true' || v === '1'
    } else {
      out[p.name] = v
    }
  }
  return out
}

async function call(capName, tool) {
  const args = coerceArgs(tool.parameters, formValues[formKey(capName, tool.name)] || {})
  busy.value = true
  const start = Date.now()
  const log = {
    capName, tool: tool.name, args,
    status: 'running', time: new Date().toLocaleTimeString(),
    duration: 0, result: '', sources: null, error: ''
  }
  logs.value.unshift(log)
  try {
    const r = await callCapabilityApi({ capability: capName, tool: tool.name, arguments: args })
    const text = r.result !== undefined ? String(r.result) : ''
    log.result = text
    // 解析 RAG 引用来源（rag_search 返回 "[N] (来源: X, 评分: Y)"）
    if (tool.name === 'rag_search') {
      const s = parseRagSources(text)
      if (s.length) log.sources = s
    }
    log.status = 'done'
  } catch (e) {
    log.status = 'error'
    log.error = e.message
  } finally {
    log.duration = Date.now() - start
    busy.value = false
  }
}

// 解析 rag_search 返回文本，提取引用来源列表
function parseRagSources(text) {
  const out = []
  if (!text) return out
  // 形如：[1] (来源: 文档名, 评分: 0.923)\n片段
  const lines = text.split('\n')
  let cur = null
  const re = /^\[(\d+)\]\s*\(来源:\s*(.+?),\s*评分:\s*([\d.]+)\)\s*$/
  for (const raw of lines) {
    const line = raw.trim()
    if (!line) continue
    const m = line.match(re)
    if (m) {
      if (cur) out.push(cur)
      cur = { seq: parseInt(m[1], 10), doc: m[2].trim(), score: parseFloat(m[3]), text: '' }
    } else if (cur) {
      cur.text += line + ' '
    }
  }
  if (cur) out.push(cur)
  return out
}

function scoreColor(score) {
  if (score >= 0.7) return 'var(--green)'
  if (score >= 0.4) return 'var(--yellow)'
  return 'var(--red)'
}

onMounted(load)
</script>

<template>
  <main class="content">
    <div class="page-title">🧩 Capability 能力实验室
      <span class="tag">M9</span>
      <span v-if="!loading" class="tag">{{ caps.length }} 能力 · {{ toolCount }} 工具</span>
    </div>

    <div v-if="errMsg" class="card"><div class="bubble error">❌ {{ errMsg }}</div></div>
    <div v-else-if="loading" class="spin">加载能力…</div>

    <!-- 空态 -->
    <div v-else-if="!caps.length" class="card">
      <div class="empty">
        <div style="font-size:40px;margin-bottom:10px">🕳️</div>
        尚未注册任何 Capability。<br>
        <span class="muted">在 config.toml 配置 <code>PMS_MCP_URL</code> 后，PMS（房卡/知识检索）能力将自动注册。</span>
      </div>
    </div>

    <!-- 能力分组 -->
    <section v-for="c in caps" :key="c.name" class="card">
      <div class="cap-head">
        <div class="cap-badge">{{ (c.name || '?')[0].toUpperCase() }}</div>
        <div>
          <div class="cap-name">{{ c.name }}</div>
          <div class="muted" style="font-size:12px">{{ c.desc }}</div>
        </div>
        <span class="tag">{{ (c.tools || []).length }} 个工具</span>
      </div>

      <div v-for="t in c.tools" :key="t.name" class="tool">
        <!-- 工具头（点击展开） -->
        <div class="tool-head" @click="toggle(c.name, t)">
          <span class="tool-ico" :class="{ rag: t.name === 'rag_search' }">
            {{ t.name === 'rag_search' ? '📚' : '🔧' }}
          </span>
          <span class="tool-name">{{ t.name }}</span>
          <span v-if="t.hints && t.hints.destructive" class="pill danger">破坏性</span>
          <span v-if="t.hints && t.hints.readOnly" class="pill">只读</span>
          <span class="chev" :class="{ open: activeTool && activeTool.key === formKey(c.name, t.name) }">▾</span>
        </div>

        <!-- 工具详情 -->
        <div v-if="activeTool && activeTool.key === formKey(c.name, t.name)" class="tool-body">
          <div class="tool-desc">{{ t.description }}</div>
          <div class="divider"></div>

          <div class="field" v-for="p in (t.parameters || [])" :key="p.name">
            <label>{{ p.name }}
              <span v-if="p.required" class="req">*</span>
              <span class="muted" style="font-weight:400">· {{ p.type }}</span>
              <span v-if="p.default !== undefined && p.default !== null" class="muted" style="font-weight:400">· 默认 {{ p.default }}</span>
            </label>
            <input class="input" :placeholder="p.description || typeHint(p)"
                   v-model="formValues[formKey(c.name, t.name)][p.name]">
            <div v-if="p.description" class="hint">{{ p.description }}</div>
          </div>

          <div v-if="!(t.parameters || []).length" class="muted" style="font-size:13px">该工具无参数，点击下方按钮直接调用。</div>

          <div style="display:flex;gap:10px;justify-content:flex-end;margin-top:6px">
            <button class="btn" @click="toggle(c.name, t)">收起</button>
            <button class="btn btn-primary" :disabled="busy" @click="call(c.name, t)">
              {{ busy ? '调用中…' : '🚀 调用' }}
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- 调用日志 -->
    <section v-if="logs.length" class="card">
      <div class="sec-title">📋 调用记录 <span class="muted">（本页会话）</span></div>
      <div class="log-list">
        <div v-for="(l, i) in logs" :key="i" class="log-item" :class="l.status">
          <div class="log-head">
            <span class="status" :class="l.status === 'done' ? 'run' : (l.status === 'error' ? 'pause' : '')">
              {{ l.status === 'done' ? '✓' : (l.status === 'error' ? '✗' : '…') }}
            </span>
            <b>{{ l.tool }}</b>
            <span class="muted" style="font-size:12px">{{ l.capName }}</span>
            <span class="muted" style="margin-left:auto;font-size:11px">{{ l.time }} · {{ l.duration }}ms</span>
          </div>

          <!-- RAG 引用来源 -->
          <div v-if="l.sources && l.sources.length" class="rag-box">
            <div class="rag-title">📚 知识库引用 <span class="muted">{{ l.sources.length }} 条</span></div>
            <div v-for="s in l.sources" :key="s.seq" class="rag-src">
              <div class="rag-meta">
                <span class="rag-seq">[{{ s.seq }}]</span>
                <span class="rag-doc">📄 {{ s.doc }}</span>
                <span class="rag-score" :style="{ color: scoreColor(s.score) }">评分 {{ s.score.toFixed(3) }}</span>
              </div>
              <div class="rag-text">{{ s.text }}</div>
            </div>
          </div>

          <!-- 普通结果（可折叠） -->
          <div v-else-if="l.result" class="result-fold">
            <div class="result-title" @click="l._open = !l._open">
              <span>{{ l._open ? '▾' : '▸' }}</span> 原始返回
            </div>
            <pre v-if="l._open" class="result-body">{{ l.result }}</pre>
          </div>

          <div v-if="l.error" class="bubble error">❌ {{ l.error }}</div>
        </div>
      </div>
    </section>
  </main>
</template>

<style scoped>
code{background:var(--bg-hover);padding:1px 6px;border-radius:5px;font-size:12px}
.bubble{font-size:14px;line-height:1.6}
.bubble.error{color:var(--red)}

.cap-head{display:flex;align-items:center;gap:12px;margin-bottom:8px}
.cap-badge{width:40px;height:40px;border-radius:12px;background:var(--accent-grad);display:grid;place-items:center;font-size:20px;font-weight:800;color:#fff;flex-shrink:0}
.cap-name{font-size:17px;font-weight:800}
.cap-head .tag{margin-left:auto}

.tool{border:1px solid var(--border);border-radius:12px;margin-top:10px;overflow:hidden;background:var(--bg-soft)}
.tool-head{display:flex;align-items:center;gap:10px;padding:13px 15px;cursor:pointer;transition:.15s;user-select:none}
.tool-head:hover{background:var(--bg-hover)}
.tool-ico{width:28px;height:28px;border-radius:8px;background:rgba(108,140,255,.15);display:grid;place-items:center;font-size:15px}
.tool-ico.rag{background:rgba(157,108,255,.16)}
.tool-name{font-weight:700;font-size:15px}
.chev{margin-left:auto;color:var(--text-faint);transition:.2s}
.chev.open{transform:rotate(180deg)}
.pill.danger{background:rgba(248,81,73,.15);color:var(--red);border-color:rgba(248,81,73,.4)}
.tool-body{padding:14px 16px;border-top:1px solid var(--border)}
.tool-desc{font-size:13px;color:var(--text-dim);line-height:1.6;white-space:pre-wrap}
.req{color:var(--red)}

/* 调用日志 */
.log-list{display:flex;flex-direction:column;gap:12px}
.log-item{border:1px solid var(--border);border-radius:12px;padding:13px 15px;background:var(--bg-soft)}
.log-item.error{border-color:rgba(248,81,73,.4)}
.log-head{display:flex;align-items:center;gap:10px;margin-bottom:8px;flex-wrap:wrap}

/* RAG 引用来源 */
.rag-box{border:1px solid var(--border);border-radius:10px;overflow:hidden}
.rag-title{font-size:13px;font-weight:700;padding:9px 12px;background:rgba(157,108,255,.1);color:#c4a8ff}
.rag-src{padding:11px 13px;border-top:1px solid var(--border)}
.rag-meta{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:5px}
.rag-seq{font-weight:800;color:var(--accent)}
.rag-doc{font-weight:600;font-size:13px}
.rag-score{font-size:12px;font-weight:700}
.rag-text{font-size:13px;color:var(--text-dim);line-height:1.65;white-space:pre-wrap}

/* 普通结果折叠 */
.result-fold{margin-top:6px}
.result-title{font-size:12px;color:var(--text-dim);cursor:pointer;padding:5px 0;user-select:none}
.result-body{font-size:12px;font-family:monospace;white-space:pre-wrap;background:var(--bg);border:1px solid var(--border);border-radius:8px;padding:10px;max-height:240px;overflow:auto;color:var(--text-dim);line-height:1.6}
</style>
