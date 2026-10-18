<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.scheduledUsers.title')"
    width="extra-wide"
    @close="handleClose"
  >
    <div class="space-y-4" data-test="account-scheduled-users">
      <div
        v-if="account"
        class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-primary-200 bg-gradient-to-r from-primary-50 to-primary-100 p-3 dark:border-primary-700/50 dark:from-primary-900/20 dark:to-primary-800/20"
      >
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary-500 to-primary-600">
            <Icon name="users" size="md" class="text-white" />
          </div>
          <div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.scheduledUsers.desc') }}</div>
          </div>
        </div>
        <div class="flex items-center gap-3">
          <div class="text-right">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.scheduledUsers.slots') }}</div>
            <div class="text-lg font-semibold text-gray-900 dark:text-gray-100" data-test="scheduled-users-slots">
              {{ slotsLabel }}
            </div>
          </div>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading"
            :title="t('common.refresh')"
            data-test="scheduled-users-refresh"
            @click="load"
          >
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>

      <div v-if="loading && !data" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <div v-else-if="users.length === 0" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400" data-test="scheduled-users-empty">
        {{ t('admin.accounts.scheduledUsers.empty') }}
      </div>

      <div v-else class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-600">
        <table class="w-full text-sm">
          <thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-700 dark:text-gray-400">
            <tr>
              <th class="px-3 py-2 text-left font-medium">{{ t('admin.accounts.scheduledUsers.user') }}</th>
              <th class="px-3 py-2 text-left font-medium">{{ t('admin.accounts.scheduledUsers.status') }}</th>
              <th class="px-3 py-2 text-right font-medium">{{ t('admin.accounts.scheduledUsers.conversations') }}</th>
              <th class="px-3 py-2 text-right font-medium" :title="t('admin.accounts.scheduledUsers.upstreamConversationsHint')">{{ t('admin.accounts.scheduledUsers.upstreamConversations') }}</th>
              <th class="px-3 py-2 text-right font-medium">{{ t('admin.accounts.scheduledUsers.requests') }}</th>
              <th class="px-3 py-2 text-left font-medium">{{ t('admin.accounts.scheduledUsers.boundAt') }}</th>
              <th class="px-3 py-2 text-left font-medium">{{ t('admin.accounts.scheduledUsers.lastActive') }}</th>
              <th class="px-3 py-2 text-right font-medium">{{ t('admin.accounts.scheduledUsers.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="user in users" :key="user.user_id" data-test="scheduled-user-row">
              <td class="px-3 py-2">
                <div class="font-medium text-gray-900 dark:text-gray-100">{{ displayName(user) }}</div>
                <div v-if="user.username && user.email" class="text-xs text-gray-500 dark:text-gray-400">{{ user.username }}</div>
              </td>
              <td class="px-3 py-2">
                <span v-if="user.bound" class="rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-500/20 dark:text-green-400">
                  {{ t('admin.accounts.scheduledUsers.bound') }}
                </span>
                <span
                  v-else-if="user.evicted_until"
                  class="rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-500/20 dark:text-amber-400"
                  :title="t('admin.accounts.scheduledUsers.evictedUntil', { time: formatDateTimeToMinute(user.evicted_until) })"
                >
                  {{ t('admin.accounts.scheduledUsers.evicted') }}
                </span>
                <span v-else class="rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600 dark:bg-dark-600 dark:text-gray-400">
                  {{ t('admin.accounts.scheduledUsers.notBound') }}
                </span>
              </td>
              <td class="px-3 py-2 text-right font-semibold text-gray-900 dark:text-gray-100">{{ user.sessions.toLocaleString() }}</td>
              <td class="px-3 py-2 text-right text-gray-700 dark:text-gray-300" data-test="upstream-conversations">{{ (user.upstream_sessions ?? 0).toLocaleString() }}</td>
              <td class="px-3 py-2 text-right text-gray-700 dark:text-gray-300">{{ user.requests.toLocaleString() }}</td>
              <td class="px-3 py-2 text-gray-600 dark:text-gray-400">{{ user.bound_at ? formatDateTimeToMinute(user.bound_at) : '-' }}</td>
              <td class="px-3 py-2 text-gray-600 dark:text-gray-400">{{ user.last_active_at ? formatDateTimeToMinute(user.last_active_at) : '-' }}</td>
              <td class="px-3 py-2 text-right">
                <button
                  v-if="user.bound"
                  type="button"
                  class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 disabled:opacity-50 dark:text-red-400 dark:hover:bg-red-900/20"
                  :disabled="evictingId !== null"
                  data-test="scheduled-user-evict"
                  @click="pendingEvict = user"
                >
                  <Icon name="trash" size="xs" />
                  {{ t('admin.accounts.scheduledUsers.evict') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <ConfirmDialog
      :show="pendingEvict !== null"
      :title="t('admin.accounts.scheduledUsers.evict')"
      :message="evictConfirmMessage"
      :confirm-text="t('admin.accounts.scheduledUsers.evict')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmEvict"
      @cancel="pendingEvict = null"
    />
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTimeToMinute } from '@/utils/format'
import type { Account, AccountScheduledUser, AccountScheduledUsersResponse } from '@/types'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const data = ref<AccountScheduledUsersResponse | null>(null)
const pendingEvict = ref<AccountScheduledUser | null>(null)
const evictingId = ref<number | null>(null)

const users = computed(() => data.value?.users ?? [])

const slotsLabel = computed(() => {
  const bound = data.value?.bound_count ?? 0
  const max = data.value?.max_users ?? 0
  return max > 0 ? `${bound} / ${max}` : `${bound} / ${t('admin.accounts.scheduledUsers.unlimited')}`
})

const displayName = (user: AccountScheduledUser) =>
  user.email || user.username || t('admin.accounts.scheduledUsers.unknownUser', { id: user.user_id })

const evictConfirmMessage = computed(() => {
  if (!pendingEvict.value) return ''
  const minutes = Math.max(1, Math.round((data.value?.evict_cooldown_seconds ?? 0) / 60))
  return t('admin.accounts.scheduledUsers.evictConfirm', { user: displayName(pendingEvict.value), minutes })
})

let loadSeq = 0
const load = async () => {
  if (!props.account) return
  const accountId = props.account.id
  const seq = ++loadSeq
  loading.value = true
  try {
    const result = await adminAPI.accounts.getScheduledUsers(accountId)
    if (seq === loadSeq) data.value = result
  } catch (error) {
    if (seq === loadSeq) appStore.showError(extractApiErrorMessage(error, t('admin.accounts.scheduledUsers.loadFailed')))
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

const confirmEvict = async () => {
  const target = pendingEvict.value
  pendingEvict.value = null
  if (!props.account || !target) return
  evictingId.value = target.user_id
  try {
    await adminAPI.accounts.evictScheduledUser(props.account.id, target.user_id)
    appStore.showSuccess(t('admin.accounts.scheduledUsers.evictSuccess'))
    await load()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.scheduledUsers.evictFailed')))
  } finally {
    evictingId.value = null
  }
}

const handleClose = () => {
  pendingEvict.value = null
  emit('close')
}

watch(
  () => [props.show, props.account?.id] as const,
  ([visible]) => {
    if (visible && props.account) {
      data.value = null
      void load()
    } else if (!visible) {
      loadSeq++
      data.value = null
      loading.value = false
    }
  },
  { immediate: true }
)
</script>
