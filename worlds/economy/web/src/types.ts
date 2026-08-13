// Economy World 观测 API 类型（对应后端 economy/server.go + economy/economy.go）

export interface AgentPublic {
  id: number
  name: string
  profession: string
  balance: number
  inventory: Record<string, number>
  skills: AgentSkill[]
  // M6.3 职业信誉
  reputation: number
  completedContracts: number
  failedContracts: number
  successRate: number
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
  minLevel: number   // M5.1 所需技能最低等级
}

export interface SkillOffer {
  skillID: string
  name: string
  description: string
  price: number
  owned: boolean
  ownLevel: number
  // M5.1 稀缺性
  owners: number
  demand: number
  scarcity: number
}

// M6.1 劳动力市场
export interface WorkerOffer {
  agentID: number
  name: string
  skillLevel: number
  successRate: number
  reputation: number
  price: number
}

export interface ServiceOffer {
  id: string
  name: string
  skill: string
  minLevel: number
  price: number
  availableWorkers: number
  workers: WorkerOffer[]   // M6.3 可雇 worker 排名（含声誉）
}

export interface ContractView {
  id: number
  employer: number
  worker: number
  service: string
  price: number
  status: string   // working / completed / failed
  duration: number // 服务执行耗时（秒）
  createdAt: number
}

export interface ContractStats {
  total: number
  completed: number
  failed: number
  pending: number
  totalVolume: number
  moneyMoved: number
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
  // M6.1 劳动力市场
  services: ServiceOffer[]
  contracts: ContractView[]
  contractStats: ContractStats
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
  // M6.3 职业信誉
  reputation: number
  completedContracts: number
  failedContracts: number
  successRate: number
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
  'contract-pay':   { label: '雇佣付款', color: '#7cc3ff', icon: '🤝' },
  'contract-refund': { label: '合约退款', color: '#ff7b72', icon: '↩️' },
}

export function txMeta(kind: string) {
  return TX_META[kind] || { label: kind, color: '#8b98b8', icon: '▪️' }
}
