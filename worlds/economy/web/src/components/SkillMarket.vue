<template>
  <el-card class="sm" shadow="never">
    <template #header>
      <div class="sm-title">🎓 技能市场 <span class="sm-sub">Agent 自主技能投资</span></div>
    </template>

    <!-- 技能价格表 -->
    <div class="sm-market">
      <div v-for="off in snapshot.skillMarket" :key="off.skillID" class="sm-card"
        :class="{ owned: off.owned }">
        <div class="sm-top">
          <span class="sm-emoji">{{ skillEmoji(off.skillID) }}</span>
          <span class="sm-name">{{ off.name }}</span>
          <span v-if="off.owned" class="sm-badge">已拥有</span>
        </div>
        <div class="sm-price">
          <span class="sm-price-num">{{ off.price }}</span> coins
        </div>
        <div class="sm-desc">{{ off.description }}</div>
      </div>
    </div>

    <!-- 购买记录流 -->
    <div class="sm-buys">
      <div class="sm-buys-title">🛒 技能购买记录</div>
      <div class="sm-buys-list" ref="scrollRef">
        <div v-for="(b, i) in buys" :key="i" class="buy-item">
          <span class="buy-emoji">{{ skillEmoji(b.skillID) }}</span>
          <span class="buy-name">{{ b.name }}</span>
          <span class="buy-skill">买了 <b>{{ b.skillID }}</b></span>
          <span class="buy-price">-{{ b.price }}</span>
        </div>
        <div v-if="!buys.length" class="buy-empty">还没有 Agent 购买技能…</div>
      </div>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { skillEmoji } from '../types'
import type { GameSnapshot, ObsEvent, SkillBuy } from '../types'

const props = defineProps<{
  snapshot: GameSnapshot
  stream: ObsEvent[]
}>()

const scrollRef = ref()

// 合并快照购买记录 + 实时 skill.buy 流
const buys = computed<SkillBuy[]>(() => {
  const fromStream: SkillBuy[] = props.stream
    .filter(ev => ev.type === 'skill.buy')
    .map(ev => ({
      agentID: ev.data?.agent ?? 0,
      name: ev.data?.name ?? '',
      skillID: ev.data?.skill ?? '',
      price: ev.data?.price ?? 0,
      balanceAt: ev.data?.balance ?? 0,
      round: ev.data?.round ?? 0,
      time: new Date(ev.time).toISOString(),
    }))
  return [...fromStream, ...props.snapshot.skillBuys].slice(0, 40)
})

watch(() => props.stream.length, async () => {
  await nextTick()
  if (scrollRef.value) scrollRef.value.scrollTop = 0
})
</script>

<style scoped>
.sm { background: #121826; border: 1px solid #2a3550; display: flex; flex-direction: column; }
.sm-title { color: #e2e9ff; font-weight: 800; }
.sm-sub { font-size: 11px; color: #6b7696; font-weight: 400; margin-left: 6px; }
.sm-market { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; padding-bottom: 8px; }
.sm-card { background: #182238; border: 1px solid #232f4a; border-radius: 8px; padding: 8px 10px; }
.sm-card.owned { opacity: 0.55; border-color: #2a3550; }
.sm-top { display: flex; align-items: center; gap: 6px; }
.sm-emoji { font-size: 14px; }
.sm-name { color: #c6d0e8; font-size: 12px; font-weight: 700; flex: 1; }
.sm-badge { font-size: 10px; color: #9ee6b0; background: #14341f; padding: 1px 6px; border-radius: 10px; }
.sm-price { color: #ffd166; font-size: 14px; font-weight: 800; margin: 4px 0 2px; }
.sm-price-num { font-size: 16px; }
.sm-desc { font-size: 10px; color: #7a86a6; }

.sm-buys { flex: 1; display: flex; flex-direction: column; min-height: 0; border-top: 1px solid #232f4a; padding-top: 8px; }
.sm-buys-title { color: #9fb3e8; font-size: 12px; font-weight: 700; margin-bottom: 4px; }
.sm-buys-list { display: flex; flex-direction: column; gap: 3px; overflow-y: auto; flex: 1; }
.buy-item { display: flex; align-items: center; gap: 6px; padding: 3px 6px; font-size: 11px; color: #c6d0e8; background: #161d33; border-radius: 6px; }
.buy-emoji { flex-shrink: 0; }
.buy-name { color: #e2e9ff; font-weight: 600; flex-shrink: 0; }
.buy-skill { flex: 1; }
.buy-skill b { color: #7cc3ff; text-transform: capitalize; }
.buy-price { color: #ff7b72; font-weight: 700; flex-shrink: 0; }
.buy-empty { color: #5a6278; text-align: center; padding: 14px 0; font-size: 12px; }
</style>
