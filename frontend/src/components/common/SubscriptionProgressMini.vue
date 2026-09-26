<template>
  <div v-if="subscriptionFeatureEnabled && hasActiveSubscriptions" class="relative" ref="containerRef">
    <!-- Mini Progress Display -->
    <button
      @click="toggleTooltip"
      class="flex cursor-pointer items-center gap-2 rounded-xl bg-purple-50 px-3 py-1.5 transition-colors hover:bg-purple-100 dark:bg-purple-900/20 dark:hover:bg-purple-900/30"
      :title="t('subscriptionProgress.viewDetails')"
    >
      <Icon name="creditCard" size="sm" class="text-purple-600 dark:text-purple-400" />
      <div class="flex items-center gap-1.5">
        <!-- Combined progress indicator -->
        <div class="flex items-center gap-0.5">
          <div
            v-for="(sub, index) in displaySubscriptions.slice(0, 3)"
            :key="index"
            class="h-2 w-2 rounded-full"
            :class="getProgressDotClass(sub)"
          ></div>
        </div>
        <span class="text-xs font-medium text-purple-700 dark:text-purple-300">
          {{ activeSubscriptions.length }}
        </span>
      </div>
    </button>

    <!-- Hover/Click Tooltip -->
    <transition name="dropdown">
      <div
        v-if="tooltipOpen"
        class="absolute right-0 z-50 mt-2 w-[340px] overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="border-b border-gray-100 p-3 dark:border-dark-700">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ t('subscriptionProgress.title') }}
          </h3>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
            {{ t('subscriptionProgress.activeCount', { count: activeSubscriptions.length }) }}
          </p>
        </div>

        <div class="max-h-64 overflow-y-auto">
          <div
            v-for="subscription in displaySubscriptions"
            :key="subscription.id"
            class="border-b border-gray-50 p-3 last:border-b-0 dark:border-dark-700/50"
          >
            <div class="mb-2 flex items-center justify-between">
              <div class="min-w-0">
                <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">
                  {{ getSubscriptionName(subscription) }}
                </span>
                <span class="mt-0.5 block break-words text-[11px] text-gray-500 dark:text-dark-400">
                  {{ getSubscriptionGroupNames(subscription).join('、') }}
                </span>
              </div>
              <span
                v-if="subscription.expires_at"
                class="text-xs"
                :class="getDaysRemainingClass(subscription.expires_at)"
              >
                {{ formatDaysRemaining(subscription.expires_at) }}
              </span>
            </div>

            <!-- Progress bars or Unlimited badge -->
            <div class="space-y-1.5">
              <!-- Unlimited subscription badge -->
              <div
                v-if="isUnlimited(subscription)"
                class="flex items-center gap-2 rounded-lg bg-gradient-to-r from-emerald-50 to-teal-50 px-2.5 py-1.5 dark:from-emerald-900/20 dark:to-teal-900/20"
              >
                <span class="text-lg text-emerald-600 dark:text-emerald-400">∞</span>
                <span class="text-xs font-medium text-emerald-700 dark:text-emerald-300">
                  {{ t('subscriptionProgress.unlimited') }}
                </span>
              </div>

              <!-- Progress bars for limited subscriptions -->
              <template v-else>
                <div v-if="getDisplayLimit(subscription, 'daily')" class="flex items-center gap-2">
                  <span class="w-8 flex-shrink-0 text-[10px] text-gray-500">{{
                    t('subscriptionProgress.daily')
                  }}</span>
                  <div class="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="
                        getProgressBarClass(
                          getDisplayUsage(subscription, 'daily'),
                          getDisplayLimit(subscription, 'daily')
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          getDisplayUsage(subscription, 'daily'),
                          getDisplayLimit(subscription, 'daily')
                        )
                      }"
                    ></div>
                  </div>
                  <span class="w-24 flex-shrink-0 text-right text-[10px] text-gray-500">
                    {{
                      formatUsage(getDisplayUsage(subscription, 'daily'), getDisplayLimit(subscription, 'daily'))
                    }}
                  </span>
                </div>

                <div v-if="getDisplayLimit(subscription, 'weekly')" class="flex items-center gap-2">
                  <span class="w-8 flex-shrink-0 text-[10px] text-gray-500">{{
                    t('subscriptionProgress.weekly')
                  }}</span>
                  <div class="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="
                        getProgressBarClass(
                          getDisplayUsage(subscription, 'weekly'),
                          getDisplayLimit(subscription, 'weekly')
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          getDisplayUsage(subscription, 'weekly'),
                          getDisplayLimit(subscription, 'weekly')
                        )
                      }"
                    ></div>
                  </div>
                  <span class="w-24 flex-shrink-0 text-right text-[10px] text-gray-500">
                    {{
                      formatUsage(getDisplayUsage(subscription, 'weekly'), getDisplayLimit(subscription, 'weekly'))
                    }}
                  </span>
                </div>

                <div v-if="getDisplayLimit(subscription, 'monthly')" class="flex items-center gap-2">
                  <span class="w-8 flex-shrink-0 text-[10px] text-gray-500">{{
                    t('subscriptionProgress.monthly')
                  }}</span>
                  <div class="h-1.5 min-w-0 flex-1 rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-1.5 rounded-full transition-all"
                      :class="
                        getProgressBarClass(
                          getDisplayUsage(subscription, 'monthly'),
                          getDisplayLimit(subscription, 'monthly')
                        )
                      "
                      :style="{
                        width: getProgressWidth(
                          getDisplayUsage(subscription, 'monthly'),
                          getDisplayLimit(subscription, 'monthly')
                        )
                      }"
                    ></div>
                  </div>
                  <span class="w-24 flex-shrink-0 text-right text-[10px] text-gray-500">
                    {{
                      formatUsage(getDisplayUsage(subscription, 'monthly'), getDisplayLimit(subscription, 'monthly'))
                    }}
                  </span>
                </div>
              </template>
            </div>
          </div>
        </div>

        <div class="border-t border-gray-100 p-2 dark:border-dark-700">
          <router-link
            to="/subscriptions"
            @click="closeTooltip"
            class="block w-full py-1 text-center text-xs text-primary-600 hover:underline dark:text-primary-400"
          >
            {{ t('subscriptionProgress.viewAll') }}
          </router-link>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useSubscriptionStore } from '@/stores'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import type { UserSubscription } from '@/types'
import { getExpirationDateRelation } from '@/utils/subscriptionQuota'

const { t } = useI18n()

const subscriptionStore = useSubscriptionStore()

const containerRef = ref<HTMLElement | null>(null)
const tooltipOpen = ref(false)

// Use store data instead of local state
const activeSubscriptions = computed(() => subscriptionStore.activeSubscriptions)
const hasActiveSubscriptions = computed(() => subscriptionStore.hasActiveSubscriptions)
// 订阅功能关闭后，即使用户仍持有后台分配的订阅，顶栏也不再露出订阅进度与「查看全部订阅」入口。
const subscriptionFeatureEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.subscription))

const displaySubscriptions = computed(() => {
  // Sort by most usage (highest percentage first)
  return [...activeSubscriptions.value].sort((a, b) => {
    const aMax = getMaxUsagePercentage(a)
    const bMax = getMaxUsagePercentage(b)
    return bMax - aMax
  })
})

function getSubscriptionName(subscription: UserSubscription): string {
  return subscription.plan_name || subscription.group?.name || `Group #${subscription.group_id}`
}

function getSubscriptionGroupNames(subscription: UserSubscription): string[] {
  const groupIDs = subscription.group_ids?.length ? subscription.group_ids : [subscription.group_id]
  return groupIDs.map((groupID, index) => subscription.group_names?.[index] || `Group #${groupID}`)
}

function hasPlanLimits(sub: UserSubscription): boolean {
  return (
    isPositiveLimit(sub.plan_daily_limit_usd) ||
    isPositiveLimit(sub.plan_weekly_limit_usd) ||
    isPositiveLimit(sub.plan_monthly_limit_usd)
  )
}

function getMaxUsagePercentage(sub: UserSubscription): number {
  const percentages: number[] = []

  const periods = [
    {
      usage: sub.daily_usage_usd,
      planLimit: sub.plan_daily_limit_usd,
      groupLimitKey: 'daily_limit_usd' as const,
      fallbackLimit: sub.daily_limit_usd,
    },
    {
      usage: sub.weekly_usage_usd,
      planLimit: sub.plan_weekly_limit_usd,
      groupLimitKey: 'weekly_limit_usd' as const,
      fallbackLimit: sub.weekly_limit_usd,
    },
    {
      usage: sub.monthly_usage_usd,
      planLimit: sub.plan_monthly_limit_usd,
      groupLimitKey: 'monthly_limit_usd' as const,
      fallbackLimit: sub.monthly_limit_usd,
    },
  ]

  for (const period of periods) {
    if (hasPlanLimits(sub)) {
      if (isPositiveLimit(period.planLimit)) {
        percentages.push(((period.usage || 0) / period.planLimit) * 100)
      }
      continue
    }

    const groupPercentages = (sub.group_limits || [])
      .map((groupLimit) => {
        const usage = sub.group_usages?.find((item) => item.group_id === groupLimit.group_id)
        const used = usage
          ? period.groupLimitKey === 'daily_limit_usd'
            ? usage.daily_usage_usd
            : period.groupLimitKey === 'weekly_limit_usd'
              ? usage.weekly_usage_usd
              : usage.monthly_usage_usd
          : period.usage
        return { limit: groupLimit[period.groupLimitKey], used }
      })
      .filter((item): item is { limit: number; used: number } => isPositiveLimit(item.limit))
      .map((item) => ((item.used || 0) / item.limit) * 100)
    if (groupPercentages.length > 0) {
      percentages.push(Math.max(...groupPercentages))
    } else if (isPositiveLimit(period.fallbackLimit)) {
      const displayPeriod = period.groupLimitKey === 'daily_limit_usd'
        ? 'daily'
        : period.groupLimitKey === 'weekly_limit_usd'
          ? 'weekly'
          : 'monthly'
      percentages.push((getDisplayUsage(sub, displayPeriod) / period.fallbackLimit) * 100)
    }
  }
  return percentages.length > 0 ? Math.max(...percentages) : 0
}

function isPositiveLimit(value: number | null | undefined): value is number {
  return typeof value === 'number' && value > 0
}

function isUnlimited(sub: UserSubscription): boolean {
  if (hasPlanLimits(sub)) return false
  return (
    !isPositiveLimit(sub.plan_daily_limit_usd) &&
    !isPositiveLimit(sub.plan_weekly_limit_usd) &&
    !isPositiveLimit(sub.plan_monthly_limit_usd) &&
    !(sub.group_limits || []).some((group) =>
      isPositiveLimit(group.daily_limit_usd) ||
      isPositiveLimit(group.weekly_limit_usd) ||
      isPositiveLimit(group.monthly_limit_usd)
    ) &&
    !isPositiveLimit(sub.group?.daily_limit_usd) &&
    !isPositiveLimit(sub.group?.weekly_limit_usd) &&
    !isPositiveLimit(sub.group?.monthly_limit_usd)
  )
}

type DisplayPeriod = 'daily' | 'weekly' | 'monthly'

function getDisplayGroupLimit(sub: UserSubscription, period: DisplayPeriod): number | null {
  const groupLimit = sub.group_limits?.find((item) => item.group_id === sub.group_id)
  const value = period === 'daily'
    ? groupLimit?.daily_limit_usd ?? sub.group?.daily_limit_usd
    : period === 'weekly'
      ? groupLimit?.weekly_limit_usd ?? sub.group?.weekly_limit_usd
      : groupLimit?.monthly_limit_usd ?? sub.group?.monthly_limit_usd
  return isPositiveLimit(value) ? value : null
}

function getDisplayLimit(sub: UserSubscription, period: DisplayPeriod): number | null {
  const planLimit = period === 'daily'
    ? sub.plan_daily_limit_usd
    : period === 'weekly'
      ? sub.plan_weekly_limit_usd
      : sub.plan_monthly_limit_usd
  if (hasPlanLimits(sub)) return isPositiveLimit(planLimit) ? planLimit : null
  return getDisplayGroupLimit(sub, period)
}

function getDisplayUsage(sub: UserSubscription, period: DisplayPeriod): number {
  if (hasPlanLimits(sub)) {
    return period === 'daily' ? sub.daily_usage_usd : period === 'weekly' ? sub.weekly_usage_usd : sub.monthly_usage_usd
  }
  const usage = sub.group_usages?.find((item) => item.group_id === sub.group_id)
  if (!usage) {
    return period === 'daily' ? sub.daily_usage_usd : period === 'weekly' ? sub.weekly_usage_usd : sub.monthly_usage_usd
  }
  return period === 'daily' ? usage.daily_usage_usd : period === 'weekly' ? usage.weekly_usage_usd : usage.monthly_usage_usd
}

function getProgressDotClass(sub: UserSubscription): string {
  // Unlimited subscriptions get a special color
  if (isUnlimited(sub)) {
    return 'bg-emerald-500'
  }
  const maxPercentage = getMaxUsagePercentage(sub)
  if (maxPercentage >= 90) return 'bg-red-500'
  if (maxPercentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const percentage = ((used || 0) / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return '0%'
  const percentage = Math.min(((used || 0) / limit) * 100, 100)
  return `${percentage}%`
}

function formatUsage(used: number | undefined, limit: number | null | undefined): string {
  const usedValue = (used || 0).toFixed(2)
  const limitValue = limit?.toFixed(2) || '∞'
  return `$${usedValue}/$${limitValue}`
}

function formatDaysRemaining(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const relation = getExpirationDateRelation(expires, now)
  if (relation === 'expired') return t('subscriptionProgress.expired')
  if (relation === 'today') return t('subscriptionProgress.expiresToday')
  if (relation === 'tomorrow') return t('subscriptionProgress.expiresTomorrow')
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
  return t('subscriptionProgress.daysRemaining', { days })
}

function getDaysRemainingClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
  if (days <= 3) return 'text-red-600 dark:text-red-400'
  if (days <= 7) return 'text-orange-600 dark:text-orange-400'
  return 'text-gray-500 dark:text-dark-400'
}

function toggleTooltip() {
  tooltipOpen.value = !tooltipOpen.value
}

function closeTooltip() {
  tooltipOpen.value = false
}

function handleClickOutside(event: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    closeTooltip()
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  // Trigger initial fetch if not already loaded
  // The actual data loading is handled by App.vue globally
  if (!subscriptionFeatureEnabled.value) return
  subscriptionStore.fetchActiveSubscriptions().catch((error) => {
    console.error('Failed to load subscriptions in SubscriptionProgressMini:', error)
  })
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.dropdown-enter-active,
.dropdown-leave-active {
  transition: all 0.2s ease;
}

.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: scale(0.95) translateY(-4px);
}
</style>
