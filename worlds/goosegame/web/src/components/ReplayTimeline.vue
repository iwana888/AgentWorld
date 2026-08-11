<template>
  <div class="replay-bar" v-if="hasEvents">
    <button class="rp-btn" @click="replayMode ? onExit() : onEnter()">
      {{ replayMode ? '⏹ 退出回放' : '⏪ 回放' }}
    </button>
    <!-- 自动播放 / 暂停（仅回放模式） -->
    <button v-if="replayMode" class="rp-btn play" @click="togglePlay">
      {{ playing ? '⏸ 暂停' : '▶ 播放' }}
    </button>
    <input v-if="replayMode" type="range" class="rp-range"
      :min="range[0]" :max="range[1]" :step="1"
      :value="replayTime" @input="onSeek" />
    <span v-if="replayMode" class="rp-time">{{ timeStr(replayTime) }}</span>
    <span v-if="replayMode" class="rp-phase" :class="{ meet: replayPhase === 'meeting', over: replayPhase === 'over' }">
      {{ phaseLabel }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'

const props = defineProps<{
  replayMode: boolean
  replayTime: number
  replayPhase: string
  range: [number, number]
}>()
const emit = defineEmits<{ (e: 'enter'): void; (e: 'exit'): void; (e: 'seek', t: number): void }>()

const hasEvents = computed(() => props.range[1] > props.range[0])
const phaseLabel = computed(() => {
  const p = props.replayPhase
  if (p === 'over') return '🎬 回放 · 已结束'
  if (p === 'meeting') return '🚨 回放 · 会议'
  return '🎬 回放 · 行动'
})

// ---- 自动播放 ----
const playing = ref(false)
let timer: ReturnType<typeof setInterval> | null = null
// 20 秒播完整个回放（可在播放中随时拖动/暂停）
const STEP_MS = 100

function togglePlay() {
  if (playing.value) stop()
  else start()
}
function start() {
  playing.value = true
  const span = Math.max(props.range[1] - props.range[0], 1)
  const stepPerTick = span / 200  // 200 tick * 100ms = 20s 播完
  timer = setInterval(() => {
    let next = props.replayTime + stepPerTick
    if (next >= props.range[1]) {
      emit('seek', props.range[1])
      stop()
    } else {
      emit('seek', next)
    }
  }, STEP_MS)
}
function stop() {
  playing.value = false
  if (timer) { clearInterval(timer); timer = null }
}
// 退出回放 / 手动拖动 → 停止自动播放
watch(() => props.replayMode, (v) => { if (!v) stop() })
onUnmounted(stop)

function onEnter() { stop(); emit('enter') }
function onExit() { stop(); emit('exit') }
function onSeek(e: Event) {
  stop()
  emit('seek', Number((e.target as HTMLInputElement).value))
}
function timeStr(t: number) {
  if (!t) return '--:--'
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
</script>

<style scoped>
.replay-bar { display: flex; align-items: center; gap: 10px; padding: 6px 12px;
  background: #10162a; border: 1px solid #2a3550; border-radius: 8px; }
.rp-btn { background: #1b2640; color: #7cc3ff; border: 1px solid #2f3d5e; border-radius: 6px;
  padding: 4px 10px; font-size: 12px; cursor: pointer; white-space: nowrap; }
.rp-btn:hover { background: #243050; }
.rp-btn.play { color: #9ee6b0; border-color: #2f5e46; }
.rp-range { flex: 1; accent-color: #7cc3ff; }
.rp-time { color: #c6d0e8; font-size: 12px; font-variant-numeric: tabular-nums; white-space: nowrap; }
.rp-phase { font-size: 12px; color: #7cc3ff; white-space: nowrap; }
.rp-phase.meet { color: #ffd166; }
.rp-phase.over { color: #ff9f43; }
</style>
