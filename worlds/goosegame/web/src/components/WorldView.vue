<template>
  <div class="world-view">
    <!-- SVG 地图：3 个房间 + Agent 位置 + 尸体 -->
    <svg :viewBox="'0 0 500 400'" class="map">
      <!-- 房间 -->
      <g v-for="room in rooms" :key="room.name">
        <rect :x="room.pos.x - 85" :y="room.pos.y - 50" width="170" height="100" rx="10"
          class="room" :class="{ active: snapshot.phase !== 'over' }" />
        <text :x="room.pos.x" :y="room.pos.y - 20" text-anchor="middle" class="room-name">{{ room.name }}</text>
        <!-- 房间内 Agent -->
        <template v-for="a in roomAgents(room.name)" :key="a.id">
          <g class="agent-node" @click="emit('select', a.id)">
            <circle :cx="a.pos.x" :cy="a.pos.y" :r="11" class="agent-dot"
              :class="{ dead: !a.alive, [teamClass(a.team)]: true }" />
            <text :x="a.pos.x" :y="a.pos.y + 22" text-anchor="middle" class="agent-name"
              :class="{ dead: !a.alive }">{{ a.name }}</text>
          </g>
        </template>
      </g>

      <!-- 尸体标记（房间内右上角，带底板防止被房间边框裁切） -->
      <g v-for="b in snapshot.bodies" :key="b.agentID" class="body-marker">
        <circle :cx="ROOM_POS[b.room].x + 62" :cy="ROOM_POS[b.room].y - 32" r="7" class="body-dot" />
        <rect :x="ROOM_POS[b.room].x + 62 - 16" :y="ROOM_POS[b.room].y - 23" width="32" height="12" rx="4" class="body-label-bg" />
        <text :x="ROOM_POS[b.room].x + 62" :y="ROOM_POS[b.room].y - 13" text-anchor="middle" class="body-label">尸体</text>
      </g>

      <!-- 阶段徽标 -->
      <text x="250" y="30" text-anchor="middle" class="phase-badge">
        {{ phaseLabel }}
      </text>
    </svg>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { ROOM_POS } from '../types'
import type { AgentPublic, GameSnapshot } from '../types'

const props = defineProps<{ snapshot: GameSnapshot }>()
const emit = defineEmits<{ (e: 'select', id: number): void }>()

const rooms = Object.entries(ROOM_POS).map(([name, pos]) => ({ name, pos }))

// 房间内 Agent 的分布坐标：网格排布，避免名字被房间边界遮挡。
function roomAgents(room: string) {
  const list = props.snapshot.agents.filter(a => a.room === room)
  const base = ROOM_POS[room]
  // 网格布局：1 行最多 3 个，超出换行
  const cols = 3
  return list.map((a, i) => {
    const col = i % cols
    const row = Math.floor(i / cols)
    const dx = (col - (Math.min(list.length, cols) - 1) / 2) * 56
    return {
      ...a,
      pos: { x: base.x + dx, y: base.y - 18 + row * 22 },
    }
  })
}

function teamClass(t: string) {
  return t === 'Duck' ? 'duck' : t === 'Dodo' ? 'dodo' : 'goose'
}

const phaseLabel = computed(() => {
  const p = props.snapshot
  if (p.phase === 'over') return `游戏结束 · ${p.winner} 胜利 (${p.endedBy})`
  if (p.phase === 'meeting') return `会议中 · 第 ${p.round} 回合`
  return `行动阶段 · 第 ${p.round} 回合`
})
</script>

<style scoped>
.world-view { width: 100%; height: 100%; }
.map { width: 100%; height: 100%; background: #0f1420; border-radius: 12px; }
.room { fill: #1c2536; stroke: #2e3b55; stroke-width: 2; }
.room.active { stroke: #3d4f75; }
.room-name { fill: #7d8bb0; font-size: 14px; font-weight: 600; }
.agent-dot { stroke: #fff; stroke-width: 1.5; cursor: pointer; }
.agent-dot.goose { fill: #4f9dff; }
.agent-dot.duck { fill: #e5534b; }
.agent-dot.dodo { fill: #c9a13b; }
.agent-dot.dead { fill: #444b5c; opacity: 0.4; }
.agent-name { fill: #c6d0e8; font-size: 10px; }
.agent-name.dead { fill: #5a6278; text-decoration: line-through; }
.body-dot { fill: #8a2f2f; stroke: #ff6b6b; }
.body-label-bg { fill: #1c2536; stroke: #8a2f2f; stroke-width: 1; }
.body-label { fill: #ff8a8a; font-size: 9px; }
.phase-badge { fill: #9fb3e8; font-size: 16px; font-weight: 700; }
</style>
