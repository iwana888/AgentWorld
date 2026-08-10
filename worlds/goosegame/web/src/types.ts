// 后端观测 API 的类型定义（对应 server.go 的 JSON 结构）。

export interface AgentPublic {
  id: number
  name: string
  team: string      // Goose / Duck / Dodo
  alive: boolean
  room: string      // Lobby / Kitchen / Engine
  taskDone: number
}

export interface GameSnapshot {
  phase: string     // action / meeting / over
  round: number
  winner?: string
  endedBy?: string
  agents: AgentPublic[]
  bodies: { agentID: number; room: string }[]
}

export interface ObsEvent {
  type: string      // agent.moved / task.completed / ...
  time: number      // ms timestamp
  data: Record<string, any>
}

// SSE 事件载荷的具体结构
export interface MovedData { agent: number; name: string; from: string; to: string }
export interface SpokeData { agent: number; name: string; target: number; text: string }
export interface KilledData { victim: number; name: string; room: string }
export interface VoteData { agent: number; name: string; target: number }
export interface EliminatedData { agent: number; name: string; team: string; by: string }
export interface GameEndedData { winner: string; reason: string; endedBy: string }
export interface TaskData { agent: number; name: string; room: string; progress: number }
export interface MeetingData { reason: string; round: number }

// 房间坐标（SVG 地图用）
export const ROOM_POS: Record<string, { x: number; y: number }> = {
  Lobby: { x: 250, y: 150 },
  Kitchen: { x: 120, y: 320 },
  Engine: { x: 380, y: 320 },
}
