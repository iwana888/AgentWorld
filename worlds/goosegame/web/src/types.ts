// 后端观测 API 的类型定义（对应 server.go 的 JSON 结构）。

export interface AgentPublic {
  id: number
  name: string
  team: string      // Goose / Duck / Dodo
  alive: boolean
  room: string
  taskDone: number
  // M5.1 2D 空间坐标
  x: number
  y: number
  facing: number    // 弧度，0=右，π/2=下
}

export interface BodyBrief {
  agentID: number
  room: string
}

export interface GameSnapshot {
  phase: string     // action / meeting / over
  round: number
  winner?: string
  endedBy?: string
  agents: AgentPublic[]
  bodies: BodyBrief[]
}

// ---- Agent Inspector（点击 Agent 后按需拉取的私有状态，面向调试） ----
export interface InspectorData {
  id: number
  name: string
  team: string
  alive: boolean
  room: string
  goal: string
  lastDecision: string
  lastAction: string
  belief: { agentID: number; name: string; suspicion: number }[]
  relationship: { agentID: number; name: string; goodwill: number }[]
  memory: string[]
}

export interface ObsEvent {
  type: string
  time: number
  data: Record<string, any>
}

export interface MovedData {
  agent: number
  name: string
  fromRoom: string
  from: { x: number; y: number }
  toRoom: string
  to: { x: number; y: number }
  facing: number
}
export interface SpokeData { agent: number; name: string; target: number; text: string }
export interface KilledData { victim: number; name: string; room: string }
export interface VoteData { agent: number; name: string; target: number }
export interface EliminatedData { agent: number; name: string; team: string; by: string }
export interface GameEndedData { winner: string; reason: string; endedBy: string }
export interface TaskData { agent: number; name: string; room: string; progress: number }
export interface MeetingData { reason: string; round: number }

// ================= 2D 地图布局（与后端 goose.RoomLayout 一致，画布 720x640） =================
export interface RoomRect {
  minX: number; minY: number; maxX: number; maxY: number
  doorX: number; doorY: number
  // 舱体主题：左上/右上标签（如 ⚙️ 或 🔧），用于房间内任务点装饰
}

export const MAP_W = 720
export const MAP_H = 640

export const ROOM_LAYOUT: Record<string, RoomRect> = {
  Cafeteria:  { minX: 260, minY: 200, maxX: 460, maxY: 330, doorX: 360, doorY: 330 },
  Engine:     { minX: 260, minY: 490, maxX: 460, maxY: 610, doorX: 360, doorY: 490 },
  Storage:    { minX: 290, minY: 40,  maxX: 410, maxY: 170, doorX: 360, doorY: 170 },
  Laboratory: { minX: 470, minY: 200, maxX: 660, maxY: 330, doorX: 470, doorY: 265 },
  Security:   { minX: 60,  minY: 200, maxX: 250, maxY: 330, doorX: 250, doorY: 265 },
  Corridor:   { minX: 260, minY: 350, maxX: 460, maxY: 470, doorX: 360, doorY: 350 },
}

export const ROOMS = Object.keys(ROOM_LAYOUT)

// 房间之间的连通（走廊），用于绘制连接线 / 计算移动路径
export const ROOM_CONNECTIONS: [string, string][] = [
  ['Cafeteria', 'Corridor'],
  ['Corridor', 'Engine'],
  ['Corridor', 'Storage'],
  ['Cafeteria', 'Laboratory'],
  ['Cafeteria', 'Security'],
]

// 房间主题标签（任务点 / 装饰）
export const ROOM_THEME: Record<string, string> = {
  Cafeteria: '🍽️', Engine: '⚙️', Storage: '📦',
  Laboratory: '🧪', Security: '🛡️', Corridor: '🚪',
}

export function roomCenter(room: string): { x: number; y: number } {
  const r = ROOM_LAYOUT[room]
  if (!r) return { x: 360, y: 265 }
  return { x: (r.minX + r.maxX) / 2, y: (r.minY + r.maxY) / 2 }
}

// 身份角色 emoji（普通观战模式：统一显示为"角色"，不暴露隐藏身份）
export function teamEmoji(team: string): string {
  return team === 'Duck' ? '🦆' : team === 'Dodo' ? '🕊️' : '🪿'
}
