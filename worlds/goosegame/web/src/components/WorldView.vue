<template>
  <div class="world-view">
    <svg :viewBox="`0 0 ${MAP_W} ${MAP_H}`" class="map">
      <defs>
        <radialGradient id="roomGlow" cx="50%" cy="40%">
          <stop offset="0%" stop-color="#1c2740" />
          <stop offset="100%" stop-color="#131b2e" />
        </radialGradient>
        <pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse">
          <path d="M40 0H0V40" fill="none" stroke="#151d30" stroke-width="1" />
        </pattern>
      </defs>

      <!-- 地板网格 -->
      <rect :width="MAP_W" :height="MAP_H" fill="url(#grid)" />

      <!-- 走廊连通 -->
      <g class="corridors">
        <path v-for="(c, i) in corridorPaths" :key="i" :d="c" />
      </g>

      <!-- 房间 = 空间（舱体） -->
      <g v-for="room in roomList" :key="room.name">
        <rect :x="room.r.minX" :y="room.r.minY"
          :width="room.r.maxX - room.r.minX" :height="room.r.maxY - room.r.minY" rx="16"
          class="room" :class="{ meet: room.name === meetingRoom }" />
        <!-- 房间名 -->
        <text :x="(room.r.minX + room.r.maxX) / 2" :y="room.r.minY + 22" text-anchor="middle"
          class="room-name">{{ room.name }}</text>
        <!-- 主题标签 -->
        <text :x="room.r.minX + 22" :y="room.r.maxY - 18" text-anchor="middle" class="theme-tag">
          {{ theme(room.name) }}
        </text>
        <!-- 任务点 -->
        <text :x="(room.r.minX + room.r.maxX) / 2 + 40" :y="(room.r.minY + room.r.maxY) / 2 - 8"
          class="task-point">🔧</text>
      </g>

      <!-- Agent 角色（2D 空间移动，transform 过渡） -->
      <g v-for="a in agents" :key="a.id" class="agent-node" @click="emit('select', a.id)">
        <g :transform="`translate(${a.x} ${a.y}) rotate(${deg(a.facing)})`"
           :style="{ transition: 'transform 0.8s ease-in-out' }">
          <CharacterSprite :color="colorOf(a.id)" :dead="!a.alive" :walking="a.walking" />
        </g>
        <text :x="a.x" :y="a.y + 26" text-anchor="middle" class="agent-name"
          :class="{ dead: !a.alive }">{{ a.name }}</text>
      </g>

      <!-- 尸体对象（躺倒） -->
      <g v-for="b in bodies" :key="b.agentID" class="body-marker">
        <g :transform="`translate(${bodyPos(b).x} ${bodyPos(b).y})`">
          <ellipse cx="0" cy="0" rx="14" ry="5" class="shadow" />
          <text class="body-emoji" text-anchor="middle" dominant-baseline="central">💀</text>
        </g>
      </g>

      <!-- 阶段徽标 -->
      <text :x="MAP_W / 2" y="30" text-anchor="middle" class="phase-badge" :class="phaseClass">
        {{ phaseLabel }}
      </text>
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import CharacterSprite from './CharacterSprite.vue'
import { MAP_W, MAP_H, ROOM_LAYOUT, ROOM_CONNECTIONS, ROOM_THEME, roomCenter, teamEmoji } from '../types'
import type { AgentRender } from '../composables/useGame'
import type { GameSnapshot } from '../types'

const props = defineProps<{ snapshot: GameSnapshot; agents: AgentRender[] }>()
const emit = defineEmits<{ (e: 'select', id: number): void }>()

const roomList = computed(() =>
  Object.entries(ROOM_LAYOUT).map(([name, r]) => ({ name, r }))
)

// 走廊路径：房间之间画一条中轴线连接
const corridorPaths = computed(() =>
  ROOM_CONNECTIONS.map(([a, b]) => {
    const p1 = roomCenter(a)
    const p2 = roomCenter(b)
    // 垂直中轴风格：从门到门
    return `M ${p1.x} ${p1.y} L ${p2.x} ${p2.y}`
  })
)

const bodies = computed(() => props.snapshot.bodies)

function bodyPos(b: { agentID: number; room: string }) {
  // 尸体放在房间内偏角落（左下）
  const r = ROOM_LAYOUT[b.room]
  if (!r) return { x: 360, y: 300 }
  return { x: r.minX + 34, y: r.maxY - 26 }
}

const meetingRoom = computed(() => {
  return props.snapshot.bodies.length ? props.snapshot.bodies[props.snapshot.bodies.length - 1].room : ''
})

const phaseClass = computed(() => props.snapshot.phase)
const phaseLabel = computed(() => {
  const p = props.snapshot
  if (p.phase === 'over') return `游戏结束 · ${teamEmoji(p.winner || '')} ${p.winner} 胜`
  if (p.phase === 'meeting') return `🚨 紧急会议 · 第 ${p.round} 回合`
  return `行动阶段 · 第 ${p.round} 回合`
})

function theme(name: string) {
  return ROOM_THEME[name] || ''
}
function deg(rad: number) {
  return Math.round((rad * 180) / Math.PI)
}

// 角色外观色板（按 Agent ID 分配，区分不同角色但不暴露身份）
const PALETTE = ['#4f9dff', '#e5534b', '#c9a13b', '#7cc3a8', '#b07ce0', '#e08b7c', '#6aa7ff', '#d98ce0']
function colorOf(id: number) {
  return PALETTE[id % PALETTE.length]
}
</script>

<style scoped>
.world-view { width: 100%; height: 100%; position: relative; }
.map { width: 100%; height: 100%; background: radial-gradient(circle at 50% 35%, #0f1728, #0a0f1c 75%); border-radius: 14px; }
.corridors path { stroke: #243049; stroke-width: 4; stroke-dasharray: 8 8; opacity: 0.5; }
.room { fill: url(#roomGlow); stroke: #31415f; stroke-width: 2.5; }
.room.meet { stroke: #ff9f43; filter: drop-shadow(0 0 10px rgba(255,159,67,.55)); }
.room-name { fill: #7d8bb0; font-size: 13px; font-weight: 800; letter-spacing: 2px; }
.theme-tag { font-size: 16px; opacity: 0.7; }
.task-point { font-size: 15px; opacity: 0.9; }

.agent-node { cursor: pointer; }
.agent-node text { user-select: none; }
.agent-name { fill: #c6d0e8; font-size: 10px; font-weight: 700; }
.agent-name.dead { fill: #5a6278; text-decoration: line-through; }

.shadow { fill: rgba(0,0,0,.35); }
.body-emoji { font-size: 22px; }

.phase-badge { font-size: 15px; font-weight: 800; fill: #9fb3e8; letter-spacing: 1px; }
.phase-badge.meeting { fill: #ffd166; }
.phase-badge.over { fill: #ff9f43; }
</style>
