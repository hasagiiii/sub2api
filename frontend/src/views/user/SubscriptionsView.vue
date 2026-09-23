<template>
  <AppLayout>
    <div class="space-y-6">
      <!-- Loading State -->
      <div v-if="loading" class="flex justify-center py-12">
        <div
          class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
        ></div>
      </div>

      <!-- Empty State -->
      <div v-else-if="subscriptions.length === 0" class="card p-12 text-center">
        <div
          class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700"
        >
          <Icon name="creditCard" size="xl" class="text-gray-400" />
        </div>
        <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('userSubscriptions.noActiveSubscriptions') }}
        </h3>
        <p class="text-gray-500 dark:text-dark-400">
          {{ t('userSubscriptions.noActiveSubscriptionsDesc') }}
        </p>
      </div>

      <!-- Subscriptions Grid -->
      <div v-else class="grid gap-6 lg:grid-cols-2">
        <div
          v-for="subscription in subscriptions"
          :key="subscription.id"
          class="overflow-hidden rounded-2xl border bg-white dark:bg-dark-800"
          :class="platformBorderClass(subscription.group?.platform || '')"
        >
          <!-- Header -->
          <div
            class="flex items-center justify-between border-b border-gray-100 p-4 dark:border-dark-700"
          >
            <div class="flex items-center gap-3">
              <div :class="['h-1.5 w-1.5 shrink-0 rounded-full', platformAccentDotClass(subscription.group?.platform || '')]" />
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="font-semibold text-gray-900 dark:text-white">
                    {{ getSubscriptionName(subscription) }}
                  </h3>
                  <span :class="['rounded-md border px-2 py-0.5 text-[11px] font-medium', platformBadgeClass(subscription.group?.platform || '')]">
                    {{ platformLabel(subscription.group?.platform || '') }}
                  </span>
                </div>
                <p class="mt-1 break-words text-xs text-gray-500 dark:text-dark-400">
                  {{ getSubscriptionGroupNames(subscription).join('、') }}
                </p>
                <p v-if="subscription.group?.description" class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                  {{ subscription.group.description }}
                </p>
                <div class="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-gray-500">
                  <span>{{ t('payment.planCard.rate') }}: ×{{ subscription.group?.rate_multiplier ?? 1 }}</span>
                  <span v-if="subscriptionHasPeakRate(subscription)" class="text-amber-700 dark:text-amber-300">
                    {{ t('payment.planCard.peakRate') }}: {{ subscriptionPeakRateLabel(subscription) }}
                  </span>
                </div>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <span
                :class="[
                  'rounded-full px-2 py-0.5 text-xs font-medium',
                  subscription.status === 'active'
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                    : subscription.status === 'expired'
                      ? 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-400'
                      : 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
                ]"
              >
                {{ t(`userSubscriptions.status.${subscription.status}`) }}
              </span>
              <button
                v-if="subscription.status === 'active'"
                :class="['rounded-lg px-3 py-1.5 text-xs font-semibold text-white transition-colors', platformButtonClass(subscription.group?.platform || '')]"
                @click="router.push({ path: '/purchase', query: { tab: 'subscription', group: String(subscription.group_id) } })"
              >
                {{ t('payment.renewNow') }}
              </button>
            </div>
          </div>

          <!-- Usage Progress -->
          <div class="space-y-4 p-4">
            <!-- Expiration Info -->
            <div v-if="subscription.expires_at" class="flex items-center justify-between text-sm">
              <span class="text-gray-500 dark:text-dark-400">{{
                t('userSubscriptions.expires')
              }}</span>
              <span :class="getExpirationClass(subscription.expires_at)">
                {{ formatExpirationDate(subscription.expires_at) }}
              </span>
            </div>
            <div v-else class="flex items-center justify-between text-sm">
              <span class="text-gray-500 dark:text-dark-400">{{
                t('userSubscriptions.expires')
              }}</span>
              <span class="text-gray-700 dark:text-gray-300">{{
                t('userSubscriptions.noExpiration')
              }}</span>
            </div>

            <div class="max-h-72 space-y-4 overflow-y-auto pr-1">
              <div
                v-for="section in getQuotaSections(subscription)"
                :key="section.key"
                class="space-y-3 rounded-xl border border-gray-200 bg-gray-50/80 p-3 shadow-sm dark:border-dark-600 dark:bg-dark-700/30"
              >
                <div class="flex items-center gap-2 border-b border-gray-200 pb-3 dark:border-dark-600">
                  <span class="h-2 w-1 shrink-0 rounded-full bg-primary-500"></span>
                  <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
                    {{ section.label }}
                  </h4>
                </div>

                <div class="space-y-3 px-1 pt-1">
                  <div v-for="row in section.rows" :key="row.key" class="space-y-2">
                    <div class="flex items-center justify-between gap-3">
                      <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ row.label }}</span>
                      <span v-if="row.limit !== null" class="shrink-0 text-sm text-gray-500 dark:text-dark-400">
                        ${{ row.used.toFixed(2) }} / ${{ row.limit.toFixed(2) }}
                      </span>
                    </div>
                    <div v-if="row.limit !== null" class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                      <div
                        class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                        :class="getProgressBarClass(row.used, row.limit)"
                        :style="{ width: getProgressWidth(row.used, row.limit) }"
                      ></div>
                    </div>
                    <p v-if="row.resetText" class="text-xs text-gray-500 dark:text-dark-400">{{ row.resetText }}</p>
                  </div>
                </div>
              </div>
              <template v-if="hasPlanLimits(subscription)">
                <div
                  v-for="section in getGroupUsageSections(subscription)"
                  :key="section.key"
                  class="space-y-3 rounded-xl border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-600 dark:bg-dark-800/60"
                >
                  <div class="flex items-center gap-2 border-b border-gray-200 pb-3 dark:border-dark-600">
                    <span class="h-2 w-1 shrink-0 rounded-full bg-gray-400 dark:bg-dark-400"></span>
                    <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">
                      {{ section.label }}
                    </h4>
                  </div>

                  <div class="space-y-3 px-1 pt-1">
                    <div v-for="row in section.rows" :key="row.key" class="space-y-2">
                      <div class="flex items-center justify-between gap-3">
                        <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ row.label }}</span>
                        <span v-if="row.limit !== null" class="shrink-0 text-sm text-gray-500 dark:text-dark-400">
                          ${{ row.used.toFixed(2) }} / ${{ row.limit.toFixed(2) }}
                        </span>
                      </div>
                      <div v-if="row.limit !== null" class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                        <div
                          class="absolute inset-y-0 left-0 rounded-full bg-gray-500 transition-all duration-300 dark:bg-gray-400"
                          :style="{ width: getProgressWidth(row.used, row.limit) }"
                        ></div>
                      </div>
                    </div>
                  </div>
                </div>
              </template>
            </div>

          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'
import subscriptionsAPI from '@/api/subscriptions'
import type { UserSubscription } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTimeToMinute } from '@/utils/format'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { platformBorderClass, platformBadgeClass, platformButtonClass, platformLabel } from '@/utils/platformColors'
import {
  getExpirationDateRelation,
  getRemainingDurationParts,
  isOneTimeDailyQuota,
  type RemainingDurationParts
} from '@/utils/subscriptionQuota'

function platformAccentDotClass(p: string): string {
  switch (p) {
    case 'anthropic': return 'bg-orange-500'
    case 'openai': return 'bg-emerald-500'
    case 'antigravity': return 'bg-purple-500'
    case 'gemini': return 'bg-blue-500'
    default: return 'bg-gray-400'
  }
}

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()

const subscriptions = ref<UserSubscription[]>([])
const loading = ref(true)

function subscriptionHasPeakRate(subscription: UserSubscription): boolean {
  return hasPeakRate(subscription.group)
}

function subscriptionPeakRateLabel(subscription: UserSubscription): string {
  return formatPeakRateWindow(subscription.group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

function getSubscriptionName(subscription: UserSubscription): string {
  return subscription.plan_name || subscription.group?.name || `Group #${subscription.group_id}`
}

function getSubscriptionGroupNames(subscription: UserSubscription): string[] {
  const groupIDs = subscription.group_ids?.length ? subscription.group_ids : [subscription.group_id]
  return groupIDs.map((groupID, index) => subscription.group_names?.[index] || `Group #${groupID}`)
}

type QuotaPeriod = 'daily' | 'weekly' | 'monthly'

interface SubscriptionQuotaRow {
  key: string
  label: string
  used: number
  limit: number | null
  resetText: string | null
}

interface SubscriptionQuotaSection {
  key: string
  label: string
  rows: SubscriptionQuotaRow[]
}

const quotaPeriodConfig: Record<QuotaPeriod, {
  planLimitKey: 'plan_daily_limit_usd' | 'plan_weekly_limit_usd' | 'plan_monthly_limit_usd'
  groupLimitKey: 'daily_limit_usd' | 'weekly_limit_usd' | 'monthly_limit_usd'
  usageKey: 'daily_usage_usd' | 'weekly_usage_usd' | 'monthly_usage_usd'
}> = {
  daily: {
    planLimitKey: 'plan_daily_limit_usd',
    groupLimitKey: 'daily_limit_usd',
    usageKey: 'daily_usage_usd',
  },
  weekly: {
    planLimitKey: 'plan_weekly_limit_usd',
    groupLimitKey: 'weekly_limit_usd',
    usageKey: 'weekly_usage_usd',
  },
  monthly: {
    planLimitKey: 'plan_monthly_limit_usd',
    groupLimitKey: 'monthly_limit_usd',
    usageKey: 'monthly_usage_usd',
  },
}

function isPositiveLimit(value: number | null | undefined): value is number {
  return typeof value === 'number' && value > 0
}

function hasPlanLimits(subscription: UserSubscription): boolean {
  return (
    isPositiveLimit(subscription.plan_daily_limit_usd) ||
    isPositiveLimit(subscription.plan_weekly_limit_usd) ||
    isPositiveLimit(subscription.plan_monthly_limit_usd)
  )
}

function getQuotaSections(subscription: UserSubscription): SubscriptionQuotaSection[] {
  const periods = Object.keys(quotaPeriodConfig) as QuotaPeriod[]
  const planRows: SubscriptionQuotaRow[] = []
  const groupLimits = subscription.group_limits || []
  const groupSections = new Map<number, SubscriptionQuotaSection>()

  // A single configured package window switches the whole subscription to
  // package mode. Unconfigured package windows are unlimited; group limits
  // must not reappear as a per-window fallback in the same subscription.
  if (hasPlanLimits(subscription)) {
    for (const period of periods) {
      const config = quotaPeriodConfig[period]
      const planLimit = subscription[config.planLimitKey]
      planRows.push({
        key: `plan-${period}`,
        label: t(`userSubscriptions.${period}`),
        used: subscription[config.usageKey] || 0,
        limit: isPositiveLimit(planLimit) ? planLimit : null,
        resetText: getQuotaResetText(subscription, period),
      })
    }
    return [{
      key: 'plan',
      label: t('userSubscriptions.planQuota'),
      rows: planRows.filter((row) => row.limit !== null),
    }].filter((section) => section.rows.length > 0)
  }

  for (const period of periods) {
    const config = quotaPeriodConfig[period]
    if (groupLimits.length === 0) {
      const fallbackLimit = subscription[config.groupLimitKey]
      if (isPositiveLimit(fallbackLimit)) {
        const groupID = subscription.group_id
        const section = getOrCreateQuotaSection(
          groupSections,
          groupID,
          subscription.group?.name || `Group #${groupID}`
        )
        section.rows.push({
          key: `${period}-${groupID}`,
          label: t(`userSubscriptions.${period}`),
          used: getGroupUsage(subscription, groupID, period),
          limit: fallbackLimit,
          resetText: getQuotaResetText(subscription, period, groupID),
        })
      }
      continue
    }

    // A period is shown for every group when at least one group has a limit
    // for that period. This keeps unlimited groups visible without mixing
    // their rows into another group's section.
    const hasGroupLimit = groupLimits.some((groupLimit) =>
      isPositiveLimit(groupLimit[config.groupLimitKey])
    )
    if (!hasGroupLimit) continue

    for (const groupLimit of groupLimits) {
      const section = getOrCreateQuotaSection(
        groupSections,
        groupLimit.group_id,
        groupLimit.name || `Group #${groupLimit.group_id}`
      )
      const groupLimitValue = groupLimit[config.groupLimitKey]
      if (!isPositiveLimit(groupLimitValue)) continue
      section.rows.push({
        key: `${period}-${groupLimit.group_id}`,
        label: t(`userSubscriptions.${period}`),
        used: getGroupUsage(subscription, groupLimit.group_id, period),
        limit: groupLimitValue,
        resetText: getQuotaResetText(subscription, period, groupLimit.group_id),
      })
    }
  }

  const sections: SubscriptionQuotaSection[] = []
  sections.push(...Array.from(groupSections.values()).filter((section) => section.rows.length > 0))
  return sections
}

function getGroupUsageSections(subscription: UserSubscription): SubscriptionQuotaSection[] {
  const groupIDs = subscription.group_ids?.length ? subscription.group_ids : [subscription.group_id]
  const groupNames = subscription.group_names || []
  const periods = Object.keys(quotaPeriodConfig) as QuotaPeriod[]

  return groupIDs.map((groupID, index) => ({
    key: `group-usage-${groupID}`,
    label: t('userSubscriptions.groupUsage', {
      group: groupNames[index] || subscription.group?.name || `Group #${groupID}`,
    }),
    rows: periods
      .map((period) => {
        const config = quotaPeriodConfig[period]
        const limit = subscription[config.planLimitKey]
        return {
          key: `group-usage-${groupID}-${period}`,
          label: t(`userSubscriptions.${period}`),
          used: getPlanGroupUsage(subscription, groupID, period),
          limit: isPositiveLimit(limit) ? limit : null,
          resetText: null,
        }
      })
      .filter((row) => row.limit !== null),
  })).filter((section) => section.rows.length > 0)
}

function getPlanGroupUsage(subscription: UserSubscription, groupID: number, period: QuotaPeriod): number {
  const usage = subscription.group_usages?.find((item) => item.group_id === groupID)
  if (usage) {
    if (period === 'daily') return usage.daily_usage_usd || 0
    if (period === 'weekly') return usage.weekly_usage_usd || 0
    return usage.monthly_usage_usd || 0
  }

  // Legacy single-group subscriptions have no separate row yet. For a
  // multi-group subscription, missing data must not duplicate the shared total
  // under every group.
  if ((subscription.group_ids?.length || 0) > 1) return 0
  return period === 'daily' ? subscription.daily_usage_usd || 0 : period === 'weekly' ? subscription.weekly_usage_usd || 0 : subscription.monthly_usage_usd || 0
}

function getOrCreateQuotaSection(
  sections: Map<number, SubscriptionQuotaSection>,
  groupID: number,
  label: string
): SubscriptionQuotaSection {
  const existing = sections.get(groupID)
  if (existing) return existing

  const section: SubscriptionQuotaSection = {
    key: `group-${groupID}`,
    label,
    rows: [],
  }
  sections.set(groupID, section)
  return section
}

function getGroupUsage(subscription: UserSubscription, groupID: number, period: QuotaPeriod): number {
  const usage = subscription.group_usages?.find((item) => item.group_id === groupID)
  if (!usage) return subscription[quotaPeriodConfig[period].usageKey] || 0
  if (period === 'daily') return usage.daily_usage_usd || 0
  if (period === 'weekly') return usage.weekly_usage_usd || 0
  return usage.monthly_usage_usd || 0
}

function getGroupWindowStart(subscription: UserSubscription, groupID: number, period: QuotaPeriod): string | null {
  const usage = subscription.group_usages?.find((item) => item.group_id === groupID)
  if (!usage) return period === 'daily' ? subscription.daily_window_start : period === 'weekly' ? subscription.weekly_window_start : subscription.monthly_window_start
  return period === 'daily' ? usage.daily_window_start || null : period === 'weekly' ? usage.weekly_window_start || null : usage.monthly_window_start || null
}

function getQuotaResetText(subscription: UserSubscription, period: QuotaPeriod, groupID?: number): string | null {
  if (period === 'daily') {
    if (groupID === undefined) return subscription.daily_window_start ? formatDailyUsageWindow(subscription) : null
    const start = getGroupWindowStart(subscription, groupID, period)
    return start ? formatResetTime(start, 24) : null
  }
  const windowStart = groupID === undefined
    ? (period === 'weekly' ? subscription.weekly_window_start : subscription.monthly_window_start)
    : getGroupWindowStart(subscription, groupID, period)
  if (!windowStart) return null
  const windowHours = period === 'weekly' ? 168 : 720
  return t('userSubscriptions.resetIn', { time: formatResetTime(windowStart, windowHours) })
}

async function loadSubscriptions() {
  try {
    loading.value = true
    subscriptions.value = await subscriptionsAPI.getMySubscriptions()
  } catch (error) {
    console.error('Failed to load subscriptions:', error)
    appStore.showError(t('userSubscriptions.failedToLoad'))
  } finally {
    loading.value = false
  }
}

function getProgressWidth(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return '0%'
  const percentage = Math.min(((used || 0) / limit) * 100, 100)
  return `${percentage}%`
}

function getProgressBarClass(used: number | undefined, limit: number | null | undefined): string {
  if (!limit || limit === 0) return 'bg-gray-400'
  const percentage = ((used || 0) / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

function formatExpirationDate(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))
  const relation = getExpirationDateRelation(expires, now)

  if (relation === null) return ''

  if (relation === 'expired') {
    return t('userSubscriptions.status.expired')
  }

  const dateStr = formatDateTimeToMinute(expires)

  if (relation === 'today') {
    return `${dateStr} (${t('common.today')})`
  }
  if (relation === 'tomorrow') {
    return `${dateStr} (${t('common.tomorrow')})`
  }

  return t('userSubscriptions.daysRemaining', { days }) + ` (${dateStr})`
}

function getExpirationClass(expiresAt: string): string {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  const days = Math.ceil(diff / (1000 * 60 * 60 * 24))

  if (diff <= 0) return 'text-red-600 dark:text-red-400 font-medium'
  if (days <= 3) return 'text-red-600 dark:text-red-400'
  if (days <= 7) return 'text-orange-600 dark:text-orange-400'
  return 'text-gray-700 dark:text-gray-300'
}

function formatDurationParts(parts: RemainingDurationParts): string {
  if (parts.days > 0) {
    return `${parts.days}d ${parts.hours}h`
  }

  if (parts.hours > 0) {
    return `${parts.hours}h ${parts.minutes}m`
  }

  return `${parts.minutes}m`
}

function formatDailyUsageWindow(subscription: UserSubscription): string {
  if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
    const parts = getRemainingDurationParts(subscription.expires_at)
    if (!parts) return t('userSubscriptions.windowNotActive')
    return t('userSubscriptions.quotaEndsIn', { time: formatDurationParts(parts) })
  }

  return t('userSubscriptions.resetIn', {
    time: formatResetTime(subscription.daily_window_start, 24)
  })
}

function formatResetTime(windowStart: string | null, windowHours: number): string {
  if (!windowStart) return t('userSubscriptions.windowNotActive')

  const start = new Date(windowStart)
  const end = new Date(start.getTime() + windowHours * 60 * 60 * 1000)
  const parts = getRemainingDurationParts(end)

  return parts ? formatDurationParts(parts) : t('userSubscriptions.windowNotActive')
}

onMounted(() => {
  loadSubscriptions()
})
</script>
