// Economy World 观测 API 类型（对应后端 economy/server.go + economy/economy.go）

export interface AgentPublic {
  id: number
  name: string
  profession: string
  balance: number
  inventory: Record<string, number>
  skills: AgentSkill[]
}

export interface Transaction {
  id: number
  time: string
  from: number
  to: number
  amount: number
  kind: string      // job-reward / purchase / sale / transfer / consume
  detail: string
}

export interface JobPublic {
  id: number
  title: string
  reward: number
  skill: string
}

export interface SkillOffer {
  skillID: string
  name: string
  description: string
  price: number
  owned: boolean
  ownLevel: number
}

export interface SkillBuy {
  agentID: number
  name: string
  skillID: string
  price: number
  balanceAt: number
  round: number
  time: string
}

export interface GameSnapshot {
  round: number
  agents: AgentPublic[]
  prices: Record<string, number>
  openJobs: JobPublic[]
  recentTx: Transaction[]
  totalWealth: number
  skillMarket: SkillOffer[]
  skillBuys: SkillBuy[]
}

export interface AgentSkill {
  skillID: string
  level: number
}

export interface InspectorData {
  id: number
  name: string
  profession: string
  personality: string
  balance: number
  inventory: Record<string, number>
  goal: string
  totalEarned: number
  totalSpent: number
  lastDecision: string
  lastAction: string
  lastWhy: string
  skills: AgentSkill[]
  skillInvested: number
  skillEarned: number
  skillReturn: number
}

// 技能 emoji / 标签
export function skillEmoji(skillID: string): string {
  const map: Record<string, string> = {
    engineer: '⚙️', farmer: '🌾', trader: '💼', courier: '📦',
    doctor: '💊', miner: '⛏️', chef: '🍳',
  }
  return map[skillID] || '🎓'
}

export interface ObsEvent {
  type: string
  time: number
  data: Record<string, any>
}

// 职业 emoji 映射
export function profEmoji(profession: string): string {
  const map: Record<string, string> = {
    Engineer: '⚙️', Farmer: '🌾', Trader: '💼', Courier: '📦',
    Doctor: '💊', Miner: '⛏️', Chef: '🍳',
  }
  return map[profession] || '👤'
}

// 交易类型标签 + 颜色
export const TX_META: Record<string, { label: string; color: string; icon: string }> = {
  'job-reward': { label: '工作收入', color: '#7cc3ff', icon: '💰' },
  'purchase':   { label: '购买', color: '#ff7b72', icon: '🛒' },
  'sale':       { label: '卖出', color: '#9ee6b0', icon: '📈' },
  'transfer':   { label: '转账', color: '#e5c07b', icon: '🔁' },
  'consume':    { label: '消费', color: '#b07ce0', icon: '🍽️' },
}

export function txMeta(kind: string) {
  return TX_META[kind] || { label: kind, color: '#8b98b8', icon: '▪️' }
}
