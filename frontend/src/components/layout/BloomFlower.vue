<template>
  <svg viewBox="0 0 40 40" fill="none" aria-hidden="true" :data-stage="stage">
    <circle cx="20" cy="20" r="20" fill="currentColor" opacity=".12" />
    <path d="M10 31Q20 28 30 31" stroke="currentColor" opacity=".5" stroke-width="1.5" stroke-linecap="round" />
    <ellipse v-if="stage === 'seed'" cx="20" cy="28" rx="3.7" ry="2.5" fill="#d28b29" transform="rotate(-20 20 28)" />
    <g v-else>
      <path d="M20 30Q19 24 20 14" stroke="#3c9f77" stroke-width="2" stroke-linecap="round" />
      <path d="M20 26Q11 26 11 19Q18 19 20 26Z" fill="#53b48b" />
      <path d="M20 24Q20 18 28 18Q27 24 20 24Z" fill="#3c9f77" />
      <g v-if="stage === 'flower'">
        <ellipse
          v-for="angle in [0, 72, 144, 216, 288]"
          :key="angle"
          cx="20" cy="9" rx="3.1" ry="5"
          :transform="`rotate(${angle} 20 13)`"
          :fill="petalColor || '#ee83a5'"
        />
        <circle cx="20" cy="13" r="3" fill="#ffd064" />
      </g>
    </g>
  </svg>
</template>

<script setup lang="ts">
defineProps<{ stage: 'seed' | 'sprout' | 'flower'; petalColor?: string }>()
</script>
