<template>
  <!-- 一个统一的 2D 卡通角色 Sprite。
       普通观战模式所有角色同物种（不暴露隐藏身份），仅用颜色区分个体。
       支持：面向（由父级 rotate）、行走动画（腿部摆动）、死亡（躺倒变灰）。 -->
  <g :class="{ dead }" :style="walking ? walkAnim : {}">
    <!-- 影子 -->
    <ellipse cx="0" cy="10" rx="13" ry="4" class="shadow" />

    <!-- 身体 -->
    <ellipse cx="0" cy="1" rx="11" ry="9" :fill="color" stroke="#0c1220" stroke-width="1.5" />
    <!-- 肚皮高光 -->
    <ellipse cx="0" cy="4" rx="6" ry="5" fill="rgba(255,255,255,0.18)" />

    <!-- 头 -->
    <circle cx="0" cy="-11" r="8" :fill="color" stroke="#0c1220" stroke-width="1.5" />
    <!-- 眼睛 -->
    <circle cx="-3" cy="-12" r="1.6" fill="#0c1220" />
    <circle cx="3" cy="-12" r="1.6" fill="#0c1220" />
    <!-- 眼睛高光 -->
    <circle cx="-3.6" cy="-12.6" r="0.5" fill="#fff" />
    <circle cx="2.4" cy="-12.6" r="0.5" fill="#fff" />
    <!-- 喙/嘴（橙色，中性） -->
    <ellipse cx="0" cy="-7.5" rx="2.6" ry="1.8" fill="#ffb43d" stroke="#0c1220" stroke-width="1" />

    <!-- 腿（行走摆动） -->
    <g class="legs">
      <line x1="-4" y1="9" x2="-5" y2="16" stroke="#0c1220" stroke-width="3" stroke-linecap="round" class="leg-a" />
      <line x1="4" y1="9" x2="5" y2="16" stroke="#0c1220" stroke-width="3" stroke-linecap="round" class="leg-b" />
    </g>
  </g>
</template>

<script setup lang="ts">
defineProps<{ color: string; dead: boolean; walking: boolean }>()

const walkAnim = {
  // 行走时左右轻微摇摆，腿部交替（由 CSS 动画驱动 .leg 旋转）
  animation: 'walkBob 0.6s ease-in-out infinite',
}
</script>

<style scoped>
.shadow { fill: rgba(0,0,0,.35); }
.dead { opacity: 0.5; filter: grayscale(1); }
.dead .legs { display: none; }
/* 行走：角色整体轻微起伏 */
@keyframes walkBob {
  0%, 100% { transform: translateY(0); }
  50% { transform: translateY(-2px); }
}
/* 腿部交替摆动 */
.leg-a { animation: stepA 0.6s ease-in-out infinite; transform-origin: 0px 9px; }
.leg-b { animation: stepB 0.6s ease-in-out infinite; transform-origin: 0px 9px; }
@keyframes stepA {
  0%, 100% { transform: rotate(8deg); }
  50% { transform: rotate(-8deg); }
}
@keyframes stepB {
  0%, 100% { transform: rotate(-8deg); }
  50% { transform: rotate(8deg); }
}
</style>
