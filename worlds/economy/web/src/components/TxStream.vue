<template>
  <el-card class="tx" shadow="never">
    <template #header>
      <div class="tx-title">💳 交易流 <span class="dot" :class="{ on: connected }"></span></div>
    </template>
    <div class="tx-list" ref="scrollRef">
      <div v-for="(t, i) in txList" :key="i" class="tx-item">
        <span class="tx-icon">{{ meta(t).icon }}</span>
        <span class="tx-detail">{{ t.detail || t.kind }}</span>
        <span class="tx-amt" :style="{ color: meta(t).color }">
          {{ t.amount >= 0 ? '+' : '' }}{{ t.amount }}
        </span>
        <span class="tx-time">{{ timeStr(t.time) }}</span>
      </div>
      <div v-if="!txList.length" class="tx-empty">等待交易…</div>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { txMeta } from '../types'
import type { Transaction, ObsEvent } from '../types'

const props = defineProps<{
  recentTx: Transaction[]
  txStream: ObsEvent[]
  connected: boolean
}>()
const scrollRef = ref()

// 合并最近交易(快照) + 实时流
const txList = computed<Transaction[]>(() => {
  const fromStream: Transaction[] = props.txStream.map(ev => ({
    id: ev.data?.id ?? 0,
    time: new Date(ev.time).toISOString(),
    from: ev.data?.from ?? 0,
    to: ev.data?.to ?? 0,
    amount: ev.data?.amount ?? 0,
    kind: ev.data?.kind ?? ev.type,
    detail: ev.data?.detail ?? '',
  }))
  // 实时流在前，快照补充
  return [...fromStream, ...props.recentTx].slice(0, 60)
})

watch(() => props.txStream.length, async () => {
  await nextTick()
  if (scrollRef.value) scrollRef.value.scrollTop = 0
})

function meta(t: Transaction) { return txMeta(t.kind) }
function timeStr(iso: string) {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
</script>

<style scoped>
.tx { background: #121826; border: 1px solid #2a3550; display: flex; flex-direction: column; }
.tx-title { color: #e2e9ff; font-weight: 800; }
.dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; background: #ff7b72; margin-left: 6px; }
.dot.on { background: #7cc3ff; }
.tx-list { display: flex; flex-direction: column; gap: 2px; overflow-y: auto; flex: 1; }
.tx-item { display: flex; align-items: center; gap: 8px; padding: 4px 6px; font-size: 12px; color: #c6d0e8; }
.tx-icon { flex-shrink: 0; }
.tx-detail { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.tx-amt { font-weight: 700; flex-shrink: 0; font-variant-numeric: tabular-nums; }
.tx-time { color: #5a6278; font-size: 10px; flex-shrink: 0; }
.tx-empty { color: #5a6278; text-align: center; padding: 16px 0; }
</style>
