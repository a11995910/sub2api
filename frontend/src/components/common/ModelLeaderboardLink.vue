<template>
  <RouterLink
    to="/model-leaderboard"
    class="leaderboard-entry"
    :title="t('modelLeaderboard.title')"
    :aria-label="t('modelLeaderboard.title')"
  >
    <Icon name="trophy" size="sm" />
    <span class="hidden xl:inline">{{ t('modelLeaderboard.entry') }}</span>
    <span class="entry-star" aria-hidden="true">✦</span>
  </RouterLink>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
const { t } = useI18n()
</script>

<style scoped>
.leaderboard-entry {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 7px;
  overflow: hidden;
  padding: 7px 10px;
  border: 1px solid #b593465c;
  border-radius: 9px;
  background: linear-gradient(115deg, #211c2c, #15131e 55%, #372038);
  color: #f0ce82;
  font-size: 13px;
  font-weight: 650;
  box-shadow: inset 0 1px #fff1;
  transition:
    box-shadow 0.2s,
    border-color 0.2s;
}
.leaderboard-entry:hover {
  border-color: #d6af67;
  box-shadow: 0 0 19px #bb80ff33;
}
.leaderboard-entry:focus-visible {
  outline: 2px solid #b998f6;
  outline-offset: 3px;
}
.entry-star {
  font-size: 10px;
  animation: star-glow 3s ease-in-out infinite;
}
@keyframes star-glow {
  50% {
    opacity: 0.4;
    transform: scale(0.7);
  }
}
@media (prefers-reduced-motion: reduce) {
  .entry-star {
    animation: none;
  }
}
@media (max-width: 639px) {
  .entry-star {
    display: none;
  }
}
</style>
