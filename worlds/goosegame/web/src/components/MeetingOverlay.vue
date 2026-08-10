<template>
  <transition name="meet">
    <div v-if="show" class="meeting-overlay">
      <div class="hall">
        <div class="hall-title">
          <span class="siren">🚨</span> EMERGENCY MEETING
          <span class="reason">{{ reason }}</span>
        </div>

        <!-- 圆桌座位：存活 Agent 围坐 -->
        <div class="table">
          <svg viewBox="0 0 420 300" class="table-svg">
            <ellipse cx="210" cy="150" rx="130" ry="80" class="table-bg" />
            <g v-for="(m, i) in aliveMembers" :key="m.id"
              :transform="`translate(${seatPos(i, aliveMembers.length).x} ${seatPos(i, aliveMembers.length).y})`"
              class="seat" :class="{ talking: m.name === lastSpeaker }">
              <circle r="26" class="seat-ring" />
              <text text-anchor="middle" dominant-baseline="central" class="seat-char">{{ sprite(m) }}</text>
              <text y="34" text-anchor="middle" class="seat-name">{{ m.name }}</text>
            </g>
          </svg>
        </div>

        <!-- 发言气泡（最新发言高亮） -->
        <div class="speech-bubble" v-if="lastSpeech">
          <span class="bubble-name">{{ lastSpeech.name }}：</span>
          <span class="bubble-text">“{{ lastSpeech.text }}”</span>
        </div>

        <!-- 发言记录 -->
        <div class="speeches">
          <div v-for="(s, i) in speeches" :key="i" class="speech">
            <span class="sname">{{ s.name }}：</span>
            <span class="stext">“{{ s.text }}”</span>
          </div>
          <div v-if="speeches.length === 0" class="empty">等待发言…</div>
        </div>

        <div class="hint">🎤 全员发言完毕后进入投票…</div>
      </div>
    </div>
  </transition>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { AgentPublic } from '../types'

const props = defineProps<{
  show: boolean
  reason: string
  agents: AgentPublic[]
  speeches: { name: string; team: string; text: string; time: number }[]
}>()

const aliveMembers = computed(() => props.agents.filter(a => a.alive))
const lastSpeaker = computed(() => props.speeches.length ? props.speeches[props.speeches.length - 1].name : '')
const lastSpeech = computed(() => props.speeches.length ? props.speeches[props.speeches.length - 1] : null)

// 圆桌座位分布：围绕中心椭圆
function seatPos(i: number, total: number) {
  const cx = 210, cy = 150
  const rx = 105, ry = 60
  const angle = (i / total) * Math.PI * 2 - Math.PI / 2
  return {
    x: cx + rx * Math.cos(angle),
    y: cy + ry * Math.sin(angle),
  }
}

// 座位角色：普通模式不暴露身份，用统一中性角色
function sprite(m: AgentPublic) {
  return '🧑'
}
</script>

<style scoped>
.meeting-overlay { position: absolute; inset: 0; z-index: 20; display: flex; align-items: center; justify-content: center;
  background: rgba(5, 8, 16, 0.82); backdrop-filter: blur(4px); border-radius: 14px; }
.hall { width: 94%; max-width: 640px; background: radial-gradient(circle at 50% 0%, #1a2540, #111827 80%);
  border: 1px solid #ff9f43; border-radius: 18px; padding: 20px 26px; box-shadow: 0 0 60px rgba(255,159,67,.28); }
.hall-title { display: flex; align-items: center; justify-content: center; gap: 10px; color: #ffd166;
  font-size: 22px; font-weight: 900; letter-spacing: 2px; }
.siren { font-size: 24px; }
.reason { color: #9fb3e8; font-size: 13px; font-weight: 500; }

.table { display: flex; justify-content: center; margin: 10px 0 4px; }
.table-svg { width: 100%; max-width: 480px; }
.table-bg { fill: #1e2b4a; stroke: #3d5278; stroke-width: 2; }
.seat-ring { fill: #243049; stroke: #4a5c80; stroke-width: 1.5; }
.seat.talking .seat-ring { stroke: #ffd166; filter: drop-shadow(0 0 6px rgba(255,209,102,.6)); }
.seat-char { font-size: 26px; }
.seat-name { fill: #c6d0e8; font-size: 10px; font-weight: 600; }

.speech-bubble { margin: 6px 0 8px; padding: 8px 12px; border-radius: 10px; background: #1a2440;
  border: 1px solid #3d5278; text-align: center; font-size: 13px; }
.bubble-name { color: #9ee6b0; font-weight: 700; }
.bubble-text { color: #e2e9ff; }

.speeches { max-height: 120px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
.speech { font-size: 12px; color: #c6d0e8; padding: 4px 8px; border-radius: 6px; background: #151d30; }
.sname { color: #7cc3ff; font-weight: 700; }
.empty { color: #5a6278; text-align: center; padding: 10px 0; }
.hint { margin-top: 10px; color: #7a86a6; font-size: 12px; text-align: center; }

.meet-enter-active, .meet-leave-active { transition: opacity .35s ease, transform .35s ease; }
.meet-enter-from, .meet-leave-to { opacity: 0; transform: scale(.94); }
</style>
