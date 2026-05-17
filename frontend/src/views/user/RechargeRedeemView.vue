<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <section class="card overflow-hidden">
          <div class="flex justify-end border-b border-gray-100 px-5 py-3 dark:border-dark-700">
            <a
              :href="shopUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="btn btn-secondary inline-flex items-center gap-2"
            >
              <Icon name="externalLink" size="sm" />
              {{ t('recharge.openInNewWindow') }}
            </a>
          </div>

          <div class="relative min-h-[640px] bg-gray-50 dark:bg-dark-950">
            <div v-if="iframeLoading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/70 backdrop-blur-sm dark:bg-dark-900/70">
              <div class="flex items-center gap-3 rounded-xl bg-white px-4 py-3 text-sm text-gray-600 shadow dark:bg-dark-800 dark:text-gray-300">
                <Icon name="refresh" size="md" class="animate-spin text-emerald-500" />
                {{ t('common.loading') }}
              </div>
            </div>
            <iframe
              :src="shopUrl"
              class="h-[72vh] min-h-[640px] w-full bg-white dark:bg-dark-900"
              title="LDXP pay shop"
              allow="payment *; clipboard-write"
              referrerpolicy="strict-origin-when-cross-origin"
              @load="iframeLoading = false"
            />
          </div>
        </section>

        <aside class="space-y-4">
          <section class="card p-5">
            <div class="flex items-center gap-3">
              <span class="inline-flex h-11 w-11 items-center justify-center rounded-xl bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
                <Icon name="gem" size="lg" />
              </span>
              <div>
                <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('recharge.currentBalance') }}</p>
                <p class="text-xl font-semibold text-emerald-600 dark:text-emerald-300">
                  {{ formatSpiritStones(user?.balance || 0) }}
                </p>
              </div>
            </div>
          </section>

          <section class="card p-5">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('recharge.redeemTitle') }}</h3>
            <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400">
              {{ t('recharge.redeemDescription') }}
            </p>
            <router-link to="/redeem" class="btn btn-primary mt-4 w-full">
              <Icon name="gift" size="md" class="mr-2" />
              {{ t('recharge.goRedeem') }}
            </router-link>
          </section>

          <section class="card border-amber-200 bg-amber-50/70 p-5 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/20 dark:text-amber-200">
            <div class="flex items-start gap-3">
              <Icon name="infoCircle" size="md" class="mt-0.5 flex-shrink-0" />
              <p class="leading-6">{{ t('recharge.iframeHint') }}</p>
            </div>
          </section>
        </aside>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAuthStore } from '@/stores/auth'
import { formatSpiritStones } from '@/utils/format'

const SHOP_URL = 'https://pay.ldxp.cn/shop/R5D7AG9X'

const { t } = useI18n()
const authStore = useAuthStore()
const user = computed(() => authStore.user)
const shopUrl = SHOP_URL
const iframeLoading = ref(true)
</script>
