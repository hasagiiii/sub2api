<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <!-- Top Toolbar: Left (search + filters) / Right (actions) -->
        <div class="flex flex-wrap items-start justify-between gap-4">
          <!-- Left: Fuzzy user search + filters (wrap to multiple lines) -->
          <div class="flex flex-1 flex-wrap items-center gap-3">
            <!-- User Search -->
            <div
              class="relative w-full sm:w-64"
              data-filter-user-search
            >
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
              />
              <input
                v-model="filterUserKeyword"
                type="text"
                :placeholder="t('admin.users.searchUsers')"
                class="input pl-10 pr-8"
                @input="debounceSearchFilterUsers"
                @focus="showFilterUserDropdown = true"
              />
              <button
                v-if="selectedFilterUser"
                @click="clearFilterUser"
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                :title="t('common.clear')"
              >
                <Icon name="x" size="sm" :stroke-width="2" />
              </button>

              <!-- User Dropdown -->
              <div
                v-if="showFilterUserDropdown && (filterUserResults.length > 0 || filterUserKeyword)"
                class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
              >
                <div
                  v-if="filterUserLoading"
                  class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('common.loading') }}
                </div>
                <div
                  v-else-if="filterUserResults.length === 0 && filterUserKeyword"
                  class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('common.noOptionsFound') }}
                </div>
                <button
                  v-for="user in filterUserResults"
                  :key="user.id"
                  type="button"
                  @click="selectFilterUser(user)"
                  class="w-full px-4 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-700"
                >
                  <span class="font-medium text-gray-900 dark:text-white">{{ user.email }}</span>
                  <span class="ml-2 text-gray-500 dark:text-gray-400">#{{ user.id }}</span>
                </button>
              </div>
            </div>

            <!-- Filters -->
            <div class="w-full sm:w-40">
              <Select
                v-model="filters.status"
                :options="statusOptions"
                :placeholder="t('admin.subscriptions.allStatus')"
                @change="applyFilters"
              />
            </div>
            <div class="w-full sm:w-48">
              <Select
                v-model="filters.group_id"
                :options="groupOptions"
                :placeholder="t('admin.subscriptions.allGroups')"
                @change="applyFilters"
              />
            </div>
            <div class="w-full sm:w-40">
              <Select
                v-model="filters.platform"
                :options="platformFilterOptions"
                :placeholder="t('admin.subscriptions.allPlatforms')"
                @change="applyFilters"
              />
            </div>
          </div>

          <!-- Right: Actions -->
          <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
            <button
              @click="loadSubscriptions"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <!-- Column Settings Dropdown -->
            <div class="relative" ref="columnDropdownRef">
              <button
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
                </svg>
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <!-- Dropdown menu -->
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 z-50 mt-2 w-48 origin-top-right rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
              >
                <div class="p-2">
                  <!-- User column mode selection -->
                  <div class="mb-2 border-b border-gray-200 pb-2 dark:border-dark-700">
                    <div class="px-3 py-1 text-xs font-medium text-gray-500 dark:text-gray-400">
                      {{ t('admin.subscriptions.columns.user') }}
                    </div>
                    <button
                      @click="setUserColumnMode('email')"
                      class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    >
                      <span>{{ t('admin.users.columns.email') }}</span>
                      <Icon v-if="userColumnMode === 'email'" name="check" size="sm" class="text-primary-500" />
                    </button>
                    <button
                      @click="setUserColumnMode('username')"
                      class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    >
                      <span>{{ t('admin.users.columns.username') }}</span>
                      <Icon v-if="userColumnMode === 'username'" name="check" size="sm" class="text-primary-500" />
                    </button>
                  </div>
                  <!-- Other columns toggle -->
                  <button
                    v-for="col in toggleableColumns"
                    :key="col.key"
                    @click="toggleColumn(col.key)"
                    class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                  >
                    <span>{{ col.label }}</span>
                    <Icon v-if="isColumnVisible(col.key)" name="check" size="sm" class="text-primary-500" />
                  </button>
                </div>
              </div>
            </div>
            <button
              @click="showGuideModal = true"
              class="btn btn-secondary"
              :title="t('admin.subscriptions.guide.showGuide')"
            >
              <Icon name="questionCircle" size="md" />
            </button>
            <button @click="showAssignModal = true" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-2" />
              {{ t('admin.subscriptions.assignSubscription') }}
            </button>
          </div>
        </div>
        <div
          v-if="selectedCount > 0"
          class="mt-3 space-y-2 rounded-xl border border-primary-200 bg-primary-50 p-3 dark:border-primary-800 dark:bg-primary-900/20"
          data-test="subscription-bulk-actions"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="mr-2 text-sm font-medium text-primary-800 dark:text-primary-200">
              {{ t('admin.subscriptions.bulk.selected', { count: selectedCount }) }}
            </span>
            <button
              v-for="action in bulkActions"
              :key="action"
              type="button"
              :class="action === 'revoke' ? 'btn btn-danger btn-sm' : 'btn btn-secondary btn-sm'"
              :data-test="`bulk-${action}`"
              :disabled="loading || bulkTargets[action].length === 0"
              @click="openBulkAction(action)"
            >
              {{ t(`admin.subscriptions.bulk.${action}`) }} ({{ bulkTargets[action].length }})
            </button>
            <button type="button" class="btn btn-secondary btn-sm" @click="clearSelection">
              {{ t('admin.subscriptions.bulk.clearSelection') }}
            </button>
          </div>
          <p class="text-xs text-gray-600 dark:text-gray-400">{{ t('admin.subscriptions.bulk.selectionHint') }}</p>
        </div>
      </template>

      <!-- Subscriptions Table -->
      <template #table>
        <DataTable
          :columns="columns"
          :data="subscriptions"
          :loading="loading"
          row-key="id"
          selectable
          :selected-keys="selectedIds"
          :selection-label="getSubscriptionSelectionLabel"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
          @update:selected-keys="handleSelectedKeysUpdate"
        >
          <template #cell-user="{ row }">
            <div class="flex items-center gap-2">
              <div
                class="flex h-8 w-8 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30"
              >
                <span class="text-sm font-medium text-primary-700 dark:text-primary-300">
                  {{ row.subject_type === 'organization'
                    ? (row.organization?.name?.charAt(0).toUpperCase() || '?')
                    : userColumnMode === 'email'
                    ? (row.user?.email?.charAt(0).toUpperCase() || '?')
                    : (row.user?.username?.charAt(0).toUpperCase() || '?')
                  }}
                </span>
              </div>
              <span class="font-medium text-gray-900 dark:text-white">
                {{ row.subject_type === 'organization'
                  ? row.organization?.name
                  : userColumnMode === 'email'
                  ? (row.user?.email || t('admin.redeem.userPrefix', { id: row.user_id }))
                  : (row.user?.username || t('admin.redeem.userPrefix', { id: row.user_id }))
                }}
              </span>
              <span v-if="row.subject_type === 'organization'" class="badge badge-info text-xs">{{ t('admin.subscriptions.enterprise') }}</span>
            </div>
          </template>

          <template #cell-group="{ row }">
            <!--
              套餐订阅是一条覆盖多个分组的订阅，必须把覆盖的分组都列出来：只显示
              主分组会让管理员以为它只作用于一个分组。
            -->
            <div v-if="isSharedPlan(row)" class="flex items-center gap-2">
              <span class="font-medium text-gray-900 dark:text-white">{{ row.plan_name || groupName(row.group_id) }}</span>
            </div>
            <div v-else-if="rowGroupIds(row).length > 0" class="flex flex-wrap items-center gap-1">
              <GroupBadge
                v-for="groupId in rowGroupIds(row)"
                :key="groupId"
                :name="groupName(groupId)"
                :platform="groupPlatform(groupId)"
                :show-rate="false"
              />
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-usage="{ row }">
            <div class="box-border w-[360px] max-w-full space-y-3">
              <template v-if="isSharedPlan(row)">
                <div class="box-border w-full overflow-y-auto" style="scrollbar-gutter: stable">
                  <div class="box-border w-full rounded-xl border border-gray-200 bg-gray-50/80 p-3 shadow-sm dark:border-dark-600 dark:bg-dark-700/30">
                    <div class="flex items-center gap-2 border-b border-gray-200 pb-3 dark:border-dark-600">
                      <span class="h-2 w-1 shrink-0 rounded-full bg-primary-500" />
                      <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">{{ t('admin.subscriptions.usage') }}</h4>
                    </div>
                    <div class="box-border w-full space-y-2 px-1 pt-1">
                      <div v-for="window in (['daily', 'weekly', 'monthly'] as const)" :key="window" class="min-w-0 space-y-1">
                        <div class="flex items-center justify-between gap-1 text-[11px] text-gray-500 dark:text-dark-400">
                          <span>{{ t(`admin.subscriptions.${window}`) }}</span>
                          <span class="min-w-0 break-all text-right">{{ formatUsageLimit(rowUsage(row, window), rowLimit(row, window)) }}</span>
                        </div>
                        <div class="relative h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                          <div
                            class="absolute inset-y-0 left-0 rounded-full"
                            :class="rowLimit(row, window) ? getProgressClass(rowUsage(row, window), rowLimit(row, window)) : 'bg-emerald-500'"
                            :style="{ width: rowLimit(row, window) ? getProgressWidth(rowUsage(row, window), rowLimit(row, window)) : '0%' }"
                          />
                        </div>
                      </div>
                    </div>
                  </div>
                </div>

                <div class="space-y-2">
                  <div class="text-xs font-medium text-gray-500 dark:text-gray-400">
                    {{ t('admin.subscriptions.groupUsage') }}
                  </div>
                  <div class="box-border w-full max-h-[6.75rem] space-y-2 overflow-y-auto" style="scrollbar-gutter: stable">
                    <div
                      v-for="group in sharedPlanGroupProgress(row)"
                      :key="group.group_id"
                      class="box-border w-full rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800/60"
                    >
                      <div class="mb-1.5 truncate text-sm font-medium text-gray-700 dark:text-gray-300">{{ group.name }}</div>
                      <div class="box-border w-full space-y-2 px-1 pt-1">
                        <div v-for="window in (['daily', 'weekly', 'monthly'] as const)" :key="window" class="min-w-0 space-y-1">
                          <div class="flex items-center justify-between gap-1 text-[11px] text-gray-500 dark:text-dark-400">
                            <span>{{ t(`admin.subscriptions.${window}`) }}</span>
                            <span class="min-w-0 break-all text-right">${{ groupUsage(group, window).toFixed(2) }}</span>
                          </div>
                          <div class="relative h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                            <div
                              class="absolute inset-y-0 left-0 rounded-full bg-gray-500 dark:bg-gray-400"
                              :style="{ width: rowLimit(row, window) ? getProgressWidth(groupUsage(group, window), rowLimit(row, window)) : '0%' }"
                            />
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </template>

              <template v-else>
                <div
                  class="space-y-3 rounded-xl border border-gray-200 bg-gray-50/80 p-3 shadow-sm dark:border-dark-600 dark:bg-dark-700/30"
                  data-testid="subscription-usage-card"
                >
                  <div class="flex items-center gap-2 border-b border-gray-200 pb-3 dark:border-dark-600">
                    <span class="h-2 w-1 shrink-0 rounded-full bg-primary-500" />
                    <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
                      {{ t('admin.subscriptions.usage') }}
                    </h4>
                  </div>

                  <div class="space-y-3">
                    <div
                      v-for="window in (['daily', 'weekly', 'monthly'] as const)"
                      :key="window"
                      class="space-y-2 border-b border-gray-200 pb-3 last:border-b-0 last:pb-0 dark:border-dark-600"
                      :data-testid="`subscription-usage-${window}`"
                    >
                      <div class="flex items-center justify-between gap-3">
                        <div class="flex items-center gap-2">
                          <span class="h-2 w-1 shrink-0 rounded-full bg-primary-500" />
                          <h5 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
                            {{ t(`admin.subscriptions.${window}`) }}
                          </h5>
                        </div>
                        <span class="shrink-0 text-sm text-gray-500 dark:text-dark-400">
                          {{ formatUsageLimit(rowUsage(row, window), rowLimit(row, window)) }}
                        </span>
                      </div>

                      <div class="space-y-2 px-1 pt-1">
                        <div class="relative h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                          <div
                            class="absolute inset-y-0 left-0 rounded-full transition-all duration-300"
                            :class="getProgressClass(rowUsage(row, window), rowLimit(row, window))"
                            :style="{ width: getProgressWidth(rowUsage(row, window), rowLimit(row, window)) }"
                          />
                        </div>
                      </div>

                      <div
                        v-if="usageWindowResetText(row, window)"
                        class="flex items-center gap-1 px-1 text-xs text-blue-600 dark:text-blue-400"
                      >
                        <svg
                          class="h-3 w-3 shrink-0"
                          fill="none"
                          viewBox="0 0 24 24"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          <path
                            stroke-linecap="round"
                            stroke-linejoin="round"
                            d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                          />
                        </svg>
                        <span>{{ usageWindowResetText(row, window) }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </template>
            </div>
          </template>

          <template #cell-expires_at="{ value }">
            <div v-if="value">
              <span
                class="text-sm"
                :class="
                  isExpiringSoon(value)
                    ? 'text-orange-600 dark:text-orange-400'
                    : 'text-gray-700 dark:text-gray-300'
                "
              >
                {{ formatDateTimeToMinute(value) }}
              </span>
              <template
                v-for="remainingExpiry in [formatRemainingExpiry(value)]"
                :key="remainingExpiry ?? 'expired'"
              >
                <div v-if="remainingExpiry" class="text-xs text-gray-500">
                  {{ remainingExpiry }}
                </div>
              </template>
            </div>
            <span v-else class="text-sm text-gray-500">{{
              t('admin.subscriptions.noExpiration')
            }}</span>
          </template>

          <template #cell-status="{ value }">
            <span
              :class="[
                'badge',
                value === 'active'
                  ? 'badge-success'
                  : value === 'expired'
                    ? 'badge-warning'
                    : 'badge-danger'
              ]"
            >
              {{ t(`admin.subscriptions.status.${value}`) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                v-if="row.status === 'active' || row.status === 'expired'"
                @click="handleExtend(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
              >
                <Icon name="calendar" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.adjust') }}</span>
              </button>
              <button
                v-if="row.status === 'active'"
                @click="handleResetQuota(row)"
                :disabled="resettingQuota && resettingSubscription?.id === row.id"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-orange-50 hover:text-orange-600 dark:hover:bg-orange-900/20 dark:hover:text-orange-400 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Icon name="refresh" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.resetQuota') }}</span>
              </button>
              <button
                v-if="row.status === 'active'"
                @click="handleRevoke(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              >
                <Icon name="ban" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.revoke') }}</span>
              </button>
              <button
                v-if="row.status === 'revoked' && row.subject_type !== 'organization'"
                @click="handleRestore(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-green-50 hover:text-green-600 dark:hover:bg-green-900/20 dark:hover:text-green-400"
              >
                <Icon name="refresh" size="sm" />
                <span class="text-xs">{{ t('admin.subscriptions.restore') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.subscriptions.noSubscriptionsYet')"
              :description="t('admin.subscriptions.assignFirstSubscription')"
              :action-text="t('admin.subscriptions.assignSubscription')"
              @action="showAssignModal = true"
            />
          </template>
        </DataTable>
      </template>

      <!-- Pagination -->
      <template #pagination>
      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
      </template>
    </TablePageLayout>

    <BulkSubscriptionActionDialog
      v-if="bulkAction !== null"
      :show="true"
      :action="bulkAction"
      :subscriptions="bulkSubscriptions"
      @close="bulkAction = null"
      @completed="handleBulkCompleted"
    />

    <!-- Assign Subscription Modal -->
    <BaseDialog
      :show="showAssignModal"
      :title="t('admin.subscriptions.assignSubscription')"
      width="normal"
      :show-close-button="!submitting"
      :close-on-escape="!submitting"
      @close="closeAssignModal"
    >
      <form
        id="assign-subscription-form"
        @submit.prevent="handleAssignSubscription"
        class="space-y-5"
      >
        <label v-if="assignTarget === 'user'" class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="batchAssignEnabled" type="checkbox" :disabled="submitting" @change="resetAssignUsers" />
          {{ t('admin.subscriptions.batchAssign.enable') }}
        </label>
        <p v-if="batchAssignEnabled" class="input-hint">{{ t('admin.subscriptions.batchAssign.hint') }}</p>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.targetType') }}</label>
          <div class="inline-flex rounded-md border border-gray-300 p-0.5 dark:border-dark-600">
            <button type="button" class="rounded px-3 py-1.5 text-sm" :class="assignTarget === 'user' ? 'bg-primary-600 text-white' : 'text-gray-600 dark:text-gray-300'" @click="assignTarget = 'user'">
              {{ t('admin.subscriptions.form.personalUser') }}
            </button>
            <button type="button" class="rounded px-3 py-1.5 text-sm" :class="assignTarget === 'organization' ? 'bg-primary-600 text-white' : 'text-gray-600 dark:text-gray-300'" @click="assignTarget = 'organization'">
              {{ t('admin.subscriptions.form.enterprise') }}
            </button>
          </div>
        </div>
        <div v-if="assignTarget === 'user'">
          <label class="input-label">{{ t('admin.subscriptions.form.user') }}</label>
          <div class="relative" data-assign-user-search>
            <input
              v-model="userSearchKeyword"
              type="text"
              :disabled="submitting || (batchAssignEnabled && assignUsers.length >= 100)"
              class="input pr-8"
              :placeholder="t('admin.usage.searchUserPlaceholder')"
              @input="debounceSearchUsers"
              @focus="showUserDropdown = true"
            />
            <button
              v-if="selectedUser"
              @click="clearUserSelection"
              type="button"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
            >
              <Icon name="x" size="sm" :stroke-width="2" />
            </button>
            <!-- User Dropdown -->
            <div
              v-if="showUserDropdown && (userSearchResults.length > 0 || userSearchKeyword)"
              class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
            >
              <div
                v-if="userSearchLoading"
                class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('common.loading') }}
              </div>
              <div
                v-else-if="userSearchResults.length === 0 && userSearchKeyword"
                class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('common.noOptionsFound') }}
              </div>
              <button
                v-for="user in userSearchResults"
                :key="user.id"
                type="button"
                :disabled="submitting || (batchAssignEnabled && assignUsers.some((selected) => selected.id === user.id))"
                @click="selectUser(user)"
                class="w-full px-4 py-2 text-left text-sm hover:bg-gray-100 disabled:opacity-50 dark:hover:bg-dark-700"
              >
                <span class="font-medium text-gray-900 dark:text-white">{{ user.email }}</span>
                <span class="ml-2 text-gray-500 dark:text-gray-400">#{{ user.id }}</span>
              </button>
            </div>
          </div>
          <div v-if="batchAssignEnabled && assignUsers.length > 0" class="mt-2 space-y-2" data-test="assign-users">
            <p class="text-sm text-gray-600 dark:text-gray-400">
              {{ t('admin.subscriptions.batchAssign.selected', { count: assignUsers.length }) }}
            </p>
            <ul class="max-h-40 space-y-1 overflow-y-auto">
              <li v-for="user in assignUsers" :key="user.id" class="flex items-center justify-between gap-2 rounded-lg bg-gray-50 px-3 py-1 text-sm dark:bg-dark-700">
                <span class="truncate">{{ user.email }} <span class="text-gray-500">#{{ user.id }}</span></span>
                <button
                  type="button"
                  :disabled="submitting"
                  :aria-label="t('admin.subscriptions.batchAssign.removeUser', { email: user.email })"
                  @click="assignUsers = assignUsers.filter((selected) => selected.id !== user.id)"
                >
                  <Icon name="x" size="sm" />
                </button>
              </li>
            </ul>
          </div>
        </div>
        <div v-else>
          <label class="input-label">{{ t('admin.subscriptions.form.enterprise') }}</label>
          <Select
            v-model="assignForm.organization_id"
            :options="organizationOptions"
            :placeholder="t('admin.subscriptions.selectEnterprise')"
            :disabled="organizationsLoading"
          />
        </div>
        <!-- 按套餐分配企业时，会按套餐覆盖的每个分组各创建一条企业订阅。 -->
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.assignSource') }}</label>
          <div class="inline-flex rounded-md border border-gray-300 p-0.5 dark:border-dark-600">
            <button type="button" class="rounded px-3 py-1.5 text-sm" :class="assignSource === 'group' ? 'bg-primary-600 text-white' : 'text-gray-600 dark:text-gray-300'" @click="assignSource = 'group'">
              {{ t('admin.subscriptions.form.byGroup') }}
            </button>
            <button type="button" class="rounded px-3 py-1.5 text-sm" :class="assignSource === 'plan' ? 'bg-primary-600 text-white' : 'text-gray-600 dark:text-gray-300'" @click="assignSource = 'plan'">
              {{ t('admin.subscriptions.form.byPlan') }}
            </button>
          </div>
        </div>
        <div v-if="assignSource === 'plan'">
          <label class="input-label">{{ t('admin.subscriptions.form.plan') }}</label>
          <Select
            v-model="assignForm.plan_id"
            :options="planOptions"
            :placeholder="t('admin.subscriptions.selectPlan')"
            :disabled="plansLoading"
          />
          <!-- 列出套餐覆盖的分组，让管理员在分配前就看清这一条订阅能用在哪些分组上。 -->
          <div v-if="selectedPlan" class="mt-2 flex flex-wrap items-center gap-1">
            <GroupBadge
              v-for="group in planGroupIds(selectedPlan)"
              :key="group"
              :name="groupName(group)"
              :platform="groupPlatform(group)"
            />
          </div>
          <p class="input-hint">{{ t('admin.subscriptions.planHint') }}</p>
        </div>
        <div v-else>
          <label class="input-label">{{ t('admin.subscriptions.form.group') }}</label>
          <Select
            v-model="assignForm.group_id"
            :disabled="submitting"
            :options="subscriptionGroupOptions"
            :placeholder="t('admin.subscriptions.selectGroup')"
          >
            <template #selected="{ option }">
              <GroupBadge
                v-if="option"
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
              />
              <span v-else class="text-gray-400">{{ t('admin.subscriptions.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :description="(option as unknown as GroupOption).description"
                :selected="selected"
              />
            </template>
          </Select>
          <p class="input-hint">{{ t('admin.subscriptions.groupHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.validityDays') }}</label>
          <input v-model.number="assignForm.validity_days" type="number" min="1" max="36500" step="1" :disabled="submitting" class="input" />
          <p class="input-hint">{{ t('admin.subscriptions.validityHint') }}</p>
        </div>
        <div v-if="batchAssignResult" class="space-y-2 text-sm" role="status" data-test="batch-assign-result">
          <p>{{ t('admin.subscriptions.batchAssign.result', { success: batchAssignResult.success_count, failed: batchAssignResult.failed_count }) }}</p>
          <ul v-if="batchAssignResult.errors.length" class="max-h-40 space-y-1 overflow-y-auto text-red-600 dark:text-red-400">
            <li v-for="(error, index) in batchAssignResult.errors" :key="index">{{ error }}</li>
          </ul>
          <p v-if="batchAssignResult.failed_count > 0" class="input-hint">{{ t('admin.subscriptions.batchAssign.retryHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeAssignModal" type="button" :disabled="submitting" class="btn btn-secondary">
            {{ batchAssignResult ? t('common.close') : t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="assign-subscription-form"
            :disabled="submitting || (batchAssignEnabled && assignUsers.length === 0)"
            class="btn btn-primary"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{ submitting ? t('admin.subscriptions.assigning') : t('admin.subscriptions.assign') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Adjust Subscription Modal -->
    <BaseDialog
      :show="showExtendModal"
      :title="t('admin.subscriptions.adjustSubscription')"
      width="narrow"
      @close="closeExtendModal"
    >
      <form
        v-if="extendingSubscription"
        id="extend-subscription-form"
        @submit.prevent="handleExtendSubscription"
        class="space-y-5"
      >
        <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700">
          <p class="text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.subscriptions.adjustingFor') }}
            <span class="font-medium text-gray-900 dark:text-white">{{
              subscriptionSubjectName(extendingSubscription)
            }}</span>
          </p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.subscriptions.currentExpiration') }}:
            <span class="font-medium text-gray-900 dark:text-white">
              {{
                extendingSubscription.expires_at
                  ? formatDateTimeToMinute(extendingSubscription.expires_at)
                  : t('admin.subscriptions.noExpiration')
              }}
            </span>
          </p>
          <p v-if="extendingSubscription.expires_at" class="mt-1 text-sm text-gray-600 dark:text-gray-400">
            {{ t('admin.subscriptions.remainingDays') }}:
            <span class="font-medium text-gray-900 dark:text-white">
              {{ getDaysRemaining(extendingSubscription.expires_at) ?? 0 }}
            </span>
          </p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.adjustDays') }}</label>
          <div class="flex items-center gap-2">
            <input
              v-model.number="extendForm.days"
              type="number"
              required
              class="input text-center"
              :placeholder="t('admin.subscriptions.adjustDaysPlaceholder')"
            />
          </div>
          <p class="input-hint">{{ t('admin.subscriptions.adjustHint') }}</p>
        </div>
      </form>
      <template #footer>
        <div v-if="extendingSubscription" class="flex justify-end gap-3">
          <button @click="closeExtendModal" type="button" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="extend-subscription-form"
            :disabled="submitting"
            class="btn btn-primary"
          >
            {{ submitting ? t('admin.subscriptions.adjusting') : t('admin.subscriptions.adjust') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Revoke Confirmation Dialog -->
    <ConfirmDialog
      :show="showRevokeDialog"
      :title="t('admin.subscriptions.revokeSubscription')"
      :message="t('admin.subscriptions.revokeConfirm', { user: subscriptionSubjectName(revokingSubscription) })"
      :confirm-text="t('admin.subscriptions.revoke')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmRevoke"
      @cancel="showRevokeDialog = false"
    />

    <!-- Restore Confirmation Dialog -->
    <ConfirmDialog
      :show="showRestoreDialog"
      :title="t('admin.subscriptions.restoreSubscription')"
      :message="t('admin.subscriptions.restoreConfirm', { user: restoringSubscription?.user?.email })"
      :confirm-text="t('admin.subscriptions.restore')"
      :cancel-text="t('common.cancel')"
      @confirm="confirmRestore"
      @cancel="showRestoreDialog = false"
    />

    <!-- Reset Quota Confirmation Dialog -->
    <ConfirmDialog
      :show="showResetQuotaConfirm"
      :title="t('admin.subscriptions.resetQuotaTitle')"
      :message="t('admin.subscriptions.resetQuotaConfirm', { user: subscriptionSubjectName(resettingSubscription) })"
      :confirm-text="t('admin.subscriptions.resetQuota')"
      :cancel-text="t('common.cancel')"
      @confirm="confirmResetQuota"
      @cancel="showResetQuotaConfirm = false"
    />
    <!-- Subscription Guide Modal -->
    <teleport to="body">
      <transition name="modal">
        <div v-if="showGuideModal" class="fixed inset-0 z-50 flex items-center justify-center p-4" @mousedown.self="showGuideModal = false">
          <div class="fixed inset-0 bg-black/50" @click="showGuideModal = false"></div>
          <div class="relative max-h-[85vh] w-full max-w-2xl overflow-y-auto rounded-xl bg-white p-6 shadow-2xl dark:bg-dark-800">
            <button type="button" class="absolute right-4 top-4 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200" @click="showGuideModal = false">
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>

            <h2 class="mb-4 text-lg font-bold text-gray-900 dark:text-white">{{ t('admin.subscriptions.guide.title') }}</h2>
            <p class="mb-5 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.subscriptions.guide.subtitle') }}</p>

            <!-- Step 1 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">1</span>
                {{ t('admin.subscriptions.guide.step1.title') }}
              </h3>
              <ol class="ml-8 list-decimal space-y-1 text-sm text-gray-600 dark:text-gray-300">
                <li>{{ t('admin.subscriptions.guide.step1.line1') }}</li>
                <li>{{ t('admin.subscriptions.guide.step1.line2') }}</li>
                <li>{{ t('admin.subscriptions.guide.step1.line3') }}</li>
              </ol>
              <div class="ml-8 mt-2">
                <router-link
                  to="/admin/groups"
                  @click="showGuideModal = false"
                  class="inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                >
                  {{ t('admin.subscriptions.guide.step1.link') }}
                  <Icon name="arrowRight" size="xs" />
                </router-link>
              </div>
            </div>

            <!-- Step 2 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">2</span>
                {{ t('admin.subscriptions.guide.step2.title') }}
              </h3>
              <ol class="ml-8 list-decimal space-y-1 text-sm text-gray-600 dark:text-gray-300">
                <li>{{ t('admin.subscriptions.guide.step2.line1') }}</li>
                <li>{{ t('admin.subscriptions.guide.step2.line2') }}</li>
                <li>{{ t('admin.subscriptions.guide.step2.line3') }}</li>
              </ol>
            </div>

            <!-- Step 3 -->
            <div class="mb-5">
              <h3 class="mb-2 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white">
                <span class="flex h-6 w-6 items-center justify-center rounded-full bg-primary-100 text-xs font-bold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">3</span>
                {{ t('admin.subscriptions.guide.step3.title') }}
              </h3>
              <div class="ml-8 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
                <table class="w-full text-sm">
                  <tbody>
                    <tr v-for="(row, i) in guideActionRows" :key="i" class="border-b border-gray-100 dark:border-dark-700 last:border-0">
                      <td class="whitespace-nowrap bg-gray-50 px-3 py-2 font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-300">{{ row.action }}</td>
                      <td class="px-3 py-2 text-gray-600 dark:text-gray-400">{{ row.desc }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>

            <!-- Tip -->
            <div class="rounded-lg bg-blue-50 p-3 text-xs text-blue-700 dark:bg-blue-900/20 dark:text-blue-300">
              {{ t('admin.subscriptions.guide.tip') }}
            </div>

            <div class="mt-4 text-right">
              <button type="button" class="btn btn-primary btn-sm" @click="showGuideModal = false">{{ t('common.close') }}</button>
            </div>
          </div>
        </div>
      </transition>
    </teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { adminPaymentAPI } from '@/api/admin/payment'
import { organizationAPI } from '@/api/organization'
import type { UserSubscription, Group, GroupPlatform, SubscriptionType, AdminOrganization, OrganizationSubscription } from '@/types'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminUser } from '@/types'
import type { SimpleUser } from '@/api/admin/usage'
import type { SubscriptionBulkAction, SubscriptionBulkActionResult, BulkAssignSubscriptionResult } from '@/api/admin/subscriptions'
import { useTableSelection } from '@/composables/useTableSelection'
import BulkSubscriptionActionDialog from '@/components/admin/subscription/BulkSubscriptionActionDialog.vue'
import type { Column } from '@/components/common/types'
import { formatDateTimeToMinute } from '@/utils/format'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  getRemainingDurationParts,
  getRemainingExpiryDuration,
  isOneTimeDailyQuota,
  type RemainingDurationParts
} from '@/utils/subscriptionQuota'
import { planGroupIds } from '@/utils/subscriptionPlan'
import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'

const { t } = useI18n()
const appStore = useAppStore()

interface GroupOption {
  value: number
  label: string
  description: string | null
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  rate: number
}

// Guide modal state
const showGuideModal = ref(false)

const guideActionRows = computed(() => [
  { action: t('admin.subscriptions.guide.actions.adjust'), desc: t('admin.subscriptions.guide.actions.adjustDesc') },
  { action: t('admin.subscriptions.guide.actions.resetQuota'), desc: t('admin.subscriptions.guide.actions.resetQuotaDesc') },
  { action: t('admin.subscriptions.guide.actions.revoke'), desc: t('admin.subscriptions.guide.actions.revokeDesc') }
])

// User column display mode: 'email' or 'username'
const userColumnMode = ref<'email' | 'username'>('email')
const USER_COLUMN_MODE_KEY = 'subscription-user-column-mode'

const loadUserColumnMode = () => {
  try {
    const saved = localStorage.getItem(USER_COLUMN_MODE_KEY)
    if (saved === 'email' || saved === 'username') {
      userColumnMode.value = saved
    }
  } catch (e) {
    console.error('Failed to load user column mode:', e)
  }
}

const saveUserColumnMode = () => {
  try {
    localStorage.setItem(USER_COLUMN_MODE_KEY, userColumnMode.value)
  } catch (e) {
    console.error('Failed to save user column mode:', e)
  }
}

const setUserColumnMode = (mode: 'email' | 'username') => {
  userColumnMode.value = mode
  saveUserColumnMode()
}

// All available columns
const allColumns = computed<Column[]>(() => [
  {
    key: 'user',
    label: userColumnMode.value === 'email'
      ? t('admin.subscriptions.columns.user')
      : t('admin.users.columns.username'),
    sortable: false
  },
  { key: 'group', label: t('admin.subscriptions.columns.group'), sortable: false },
  { key: 'usage', label: t('admin.subscriptions.columns.usage'), sortable: false },
  { key: 'expires_at', label: t('admin.subscriptions.columns.expires'), sortable: true },
  { key: 'status', label: t('admin.subscriptions.columns.status'), sortable: true },
  { key: 'actions', label: t('admin.subscriptions.columns.actions'), sortable: false }
])

// Columns that can be toggled (exclude user and actions which are always visible)
const toggleableColumns = computed(() =>
  allColumns.value.filter(col => col.key !== 'user' && col.key !== 'actions')
)

// Hidden columns set
const hiddenColumns = reactive<Set<string>>(new Set())

// Default hidden columns
const DEFAULT_HIDDEN_COLUMNS: string[] = []

// localStorage key
const HIDDEN_COLUMNS_KEY = 'subscription-hidden-columns'

// Load saved column settings
const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as string[]
      parsed.forEach(key => hiddenColumns.add(key))
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach(key => hiddenColumns.add(key))
    }
  } catch (e) {
    console.error('Failed to load saved columns:', e)
    DEFAULT_HIDDEN_COLUMNS.forEach(key => hiddenColumns.add(key))
  }
}

// Save column settings to localStorage
const saveColumnsToStorage = () => {
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

// Toggle column visibility
const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  saveColumnsToStorage()
}

// Check if column is visible
const isColumnVisible = (key: string) => !hiddenColumns.has(key)

// Filtered columns for display
const columns = computed<Column[]>(() =>
  allColumns.value.filter(col =>
    col.key === 'user' || col.key === 'actions' || !hiddenColumns.has(col.key)
  )
)

// Column dropdown state
const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

// Filter options
const statusOptions = computed(() => [
  { value: '', label: t('admin.subscriptions.allStatus') },
  { value: 'active', label: t('admin.subscriptions.status.active') },
  { value: 'expired', label: t('admin.subscriptions.status.expired') },
  { value: 'revoked', label: t('admin.subscriptions.status.revoked') }
])

type AdminSubscriptionRow = UserSubscription & {
  subject_type?: 'user' | 'organization'
  organization?: { id: number; name: string; company_id?: string }
  shared_subscription_ids?: number[]
}

const subscriptions = ref<AdminSubscriptionRow[]>([])
const groups = ref<Group[]>([])
const loading = ref(false)
let abortController: AbortController | null = null

const { selectedIds, selectedCount, setSelectedIds, clear: clearSelection, removeMany: removeSelectedIds } =
  useTableSelection<UserSubscription>({ rows: subscriptions, getId: (subscription) => subscription.id })
const bulkActions: SubscriptionBulkAction[] = ['extend', 'reset_quota', 'revoke', 'restore']
const bulkAction = ref<SubscriptionBulkAction | null>(null)
const bulkSubscriptions = ref<UserSubscription[]>([])
const bulkTargets = computed(() => {
  const selected = subscriptions.value.filter((subscription) => selectedIds.value.includes(subscription.id))
  return {
    extend: selected.filter((subscription) => ['active', 'expired'].includes(subscription.status)),
    reset_quota: selected.filter((subscription) => subscription.status === 'active'),
    revoke: selected.filter((subscription) => subscription.status === 'active'),
    restore: selected.filter((subscription) => subscription.status === 'revoked')
  }
})
const getSubscriptionSelectionLabel = (subscription: UserSubscription) =>
  t('admin.subscriptions.bulk.selectSubscription', { id: subscription.id })
const handleSelectedKeysUpdate = (keys: Array<string | number>) => {
  const visibleIds = new Set(subscriptions.value.map((subscription) => subscription.id))
  setSelectedIds(keys.filter((key): key is number => typeof key === 'number' && visibleIds.has(key)))
}
const openBulkAction = (action: SubscriptionBulkAction) => {
  if (loading.value || bulkTargets.value[action].length === 0) return
  bulkSubscriptions.value = [...bulkTargets.value[action]]
  bulkAction.value = action
}
const handleBulkCompleted = async (result: SubscriptionBulkActionResult) => {
  removeSelectedIds(result.results.filter((item) => item.success).map((item) => item.subscription_id))
  await loadSubscriptions()
}

// Toolbar user filter (fuzzy search -> select user_id)
const filterUserKeyword = ref('')
const filterUserResults = ref<SimpleUser[]>([])
const filterUserLoading = ref(false)
const showFilterUserDropdown = ref(false)
const selectedFilterUser = ref<SimpleUser | null>(null)
let filterUserSearchTimeout: ReturnType<typeof setTimeout> | null = null

// User search state
const userSearchKeyword = ref('')
const userSearchResults = ref<AdminUser[]>([])
const userSearchLoading = ref(false)
const showUserDropdown = ref(false)
const selectedUser = ref<AdminUser | null>(null)
const batchAssignEnabled = ref(false)
const assignUsers = ref<AdminUser[]>([])
const batchAssignResult = ref<BulkAssignSubscriptionResult | null>(null)
let userSearchTimeout: ReturnType<typeof setTimeout> | null = null
const assignTarget = ref<'user' | 'organization'>('user')
const organizations = ref<AdminOrganization[]>([])
const organizationsLoading = ref(false)

const filters = reactive({
  status: 'active',
  group_id: '',
  platform: '',
  user_id: null as number | null
})

// Sorting state
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const showAssignModal = ref(false)
const showExtendModal = ref(false)
const showRevokeDialog = ref(false)
const showRestoreDialog = ref(false)
const showResetQuotaConfirm = ref(false)
const submitting = ref(false)
const resettingSubscription = ref<AdminSubscriptionRow | null>(null)
const resettingQuota = ref(false)
const extendingSubscription = ref<AdminSubscriptionRow | null>(null)
const revokingSubscription = ref<AdminSubscriptionRow | null>(null)
const restoringSubscription = ref<UserSubscription | null>(null)

const assignForm = reactive({
  user_id: null as number | null,
  organization_id: null as number | null,
  group_id: null as number | null,
  plan_id: null as number | null,
  validity_days: 30
})

// 分配来源：按分组给一条手动订阅，或按套餐给套餐覆盖的全部权益。
const assignSource = ref<'group' | 'plan'>('group')
const plans = ref<SubscriptionPlan[]>([])
const plansLoading = ref(false)

watch(assignTarget, target => {
  if (target === 'organization') {
    // Batch assignment only applies to personal users; discard hidden selections
    // so switching back cannot unexpectedly reuse the previous batch.
    batchAssignEnabled.value = false
    assignUsers.value = []
    batchAssignResult.value = null
    assignForm.user_id = null
  }
})

// 切换来源时清掉另一种来源的选择，避免提交时两者同时带上（后端会拒绝）。
watch(assignSource, source => {
  if (source === 'plan') assignForm.group_id = null
  else assignForm.plan_id = null
})

const planOptions = computed(() =>
  plans.value.map(plan => {
    const groupNames = planGroupIds(plan)
      .map(id => groups.value.find(group => group.id === id)?.name || `#${id}`)
      .join(' + ')
    return {
      value: plan.id,
      label: plan.name,
      description: groupNames
    }
  })
)

const selectedPlan = computed(() => plans.value.find(plan => plan.id === assignForm.plan_id) || null)

/** 选中套餐后把有效期带出来，让管理员看到默认发放的时长（仍可改）。 */
watch(
  () => assignForm.plan_id,
  () => {
    const plan = selectedPlan.value
    if (plan && plan.validity_days > 0) assignForm.validity_days = plan.validity_days
  }
)

const organizationOptions = computed(() => organizations.value.map(organization => ({
  value: organization.id,
  label: `${organization.name} (${organization.company_id || organization.account_id})`
})))

watch(showAssignModal, async show => {
  if (!show || plans.value.length > 0 || plansLoading.value) return
  plansLoading.value = true
  try {
    const res = await adminPaymentAPI.getPlans()
    plans.value = res.data || []
  } catch (error) {
    console.error('Failed to load subscription plans:', error)
  } finally {
    plansLoading.value = false
  }
})

watch(showAssignModal, async show => {
  if (!show || organizations.value.length > 0) return
  organizationsLoading.value = true
  try {
    const items: AdminOrganization[] = []
    let page = 1
    let total = 0
    do {
      const result = await organizationAPI.listOrganizations({ status: 'active', page, page_size: 100 })
      items.push(...result.items)
      total = result.total
      page += 1
      if (result.items.length === 0) break
    } while (items.length < total)
    organizations.value = items
  } catch (error) {
    console.error('Failed to load organizations:', error)
    appStore.showError(t('admin.subscriptions.failedToLoadEnterprises'))
  } finally {
    organizationsLoading.value = false
  }
})

const extendForm = reactive({
  days: 30
})

// Group options for filter (all groups)
const groupOptions = computed(() => [
  { value: '', label: t('admin.subscriptions.allGroups') },
  ...groups.value.map((g) => ({ value: g.id.toString(), label: g.name }))
])

const platformFilterOptions = computed(() => [
  { value: '', label: t('admin.subscriptions.allPlatforms') },
  ...GROUP_PLATFORM_OPTIONS
])

// Group options for assign (only subscription type groups)
const subscriptionGroupOptions = computed(() =>
  groups.value
    .filter((g) => g.subscription_type === 'subscription' && g.status === 'active')
    .map((g) => ({
      value: g.id,
      label: g.name,
      description: g.description,
      platform: g.platform,
      subscriptionType: g.subscription_type,
      rate: g.rate_multiplier
    }))
)

const groupById = computed(() => {
  const map = new Map<number, Group>()
  groups.value.forEach(group => map.set(group.id, group))
  return map
})

/** 分组名；分组列表尚未加载或分组已被删除时退回 #id，避免显示成空白。 */
const groupName = (groupId: number): string => groupById.value.get(groupId)?.name || `#${groupId}`
const groupPlatform = (groupId: number): GroupPlatform | undefined => groupById.value.get(groupId)?.platform

/**
 * 订阅覆盖的全部分组。套餐订阅是一条覆盖多个分组的订阅，只展示 `group` 这个主分组
 * 会让管理员以为它只作用于一个分组。`group_ids` 未下发时回退到主分组。
 */
const rowGroupIds = (row: AdminSubscriptionRow): number[] => {
  if (row.group_ids?.length) return row.group_ids
  return row.group_id ? [row.group_id] : []
}

type UsageWindow = 'daily' | 'weekly' | 'monthly'

/**
 * 该行用量进度的分母。
 *
 * 优先用后端解析好的生效限额。只要套餐设置了任一窗口，整条订阅进入套餐模式，其他
 * 未设置窗口为不限额，不能再回退到主分组限额。套餐订阅的额度是整个额度池共享的一份，
 * 直接读主分组的限额会算出错误的进度——分组没配限额时甚至会把有套餐限额的订阅显示成
 * "无限制"。
 * 没有套餐额度的行才回退到分组限额。
 */
const rowUsage = (row: AdminSubscriptionRow, window: UsageWindow): number => {
  const value = window === 'daily'
    ? row.daily_usage_usd
    : window === 'weekly'
      ? row.weekly_usage_usd
      : row.monthly_usage_usd
  return Number(value || 0)
}

const formatUsageLimit = (used: number, limit: number | null): string => {
  const usedText = `$${used.toFixed(2)}`
  return limit != null && limit > 0 ? `${usedText}/$${limit.toFixed(2)}` : `${usedText}/+∞`
}

const rowLimit = (row: AdminSubscriptionRow, window: UsageWindow): number | null => {
  const planLimit =
    window === 'daily'
      ? row.plan_daily_limit_usd
      : window === 'weekly'
        ? row.plan_weekly_limit_usd
        : row.plan_monthly_limit_usd
  const hasPlanLimits = [
    row.plan_daily_limit_usd,
    row.plan_weekly_limit_usd,
    row.plan_monthly_limit_usd,
  ].some((limit) => typeof limit === 'number' && limit > 0)
  if (hasPlanLimits) return planLimit ?? null

  const resolved =
    window === 'daily'
      ? row.daily_limit_usd
      : window === 'weekly'
        ? row.weekly_limit_usd
        : row.monthly_limit_usd
  if (resolved != null) return resolved
  const group = row.group
  if (!group) return null
  const fallback =
    window === 'daily'
      ? group.daily_limit_usd
      : window === 'weekly'
        ? group.weekly_limit_usd
        : group.monthly_limit_usd
  return fallback ?? null
}

const isSharedPlan = (row: AdminSubscriptionRow): boolean =>
  (row.subject_type === 'organization' || row.subject_type === 'user') && row.plan_id != null && hasPositivePlanLimit(row)

const groupUsage = (
  group: { progress: Array<{ window: UsageWindow; used: number; limit: number }> },
  window: UsageWindow,
): number => group.progress.find((item) => item.window === window)?.used || 0

const sharedPlanGroupProgress = (row: AdminSubscriptionRow) => {
  if (!isSharedPlan(row)) return []
  const groupIDs = row.group_ids?.length ? row.group_ids : [row.group_id]
  const groupNames = row.group_names || []
  const periods: UsageWindow[] = ['daily', 'weekly', 'monthly']
  return groupIDs.map((groupID, index) => {
    const usage = row.group_usages?.find((item) => item.group_id === groupID)
    const progress = periods
      .map((window) => {
        const limit = rowLimit(row, window)
        if (limit == null || limit <= 0) return null
        const used = usage
          ? window === 'daily'
            ? usage.daily_usage_usd
            : window === 'weekly'
              ? usage.weekly_usage_usd
              : usage.monthly_usage_usd
          : 0
        return { window, used: used || 0, limit }
      })
      .filter((item): item is { window: UsageWindow; used: number; limit: number } => item !== null)
    return {
      group_id: groupID,
      name: groupNames[index] || groupName(groupID),
      progress,
    }
  }).filter((group) => group.progress.length > 0)
}

const applyFilters = () => {
  clearSelection()
  pagination.page = 1
  loadSubscriptions()
}

const loadSubscriptions = async () => {
  if (abortController) {
    abortController.abort()
  }
  const requestController = new AbortController()
  abortController = requestController
  const { signal } = requestController

  loading.value = true
  try {
    const commonFilters = {
      status: (filters.status as any) || undefined,
      group_id: filters.group_id ? parseInt(filters.group_id) : undefined,
      platform: filters.platform || undefined,
      sort_by: sortState.sort_by,
      sort_order: sortState.sort_order
    }
    const userRows = await loadAllUserSubscriptionRows(commonFilters, signal)
    const organizationRows = filters.user_id || filters.status === 'revoked'
      ? []
      : await loadAllOrganizationSubscriptionRows(commonFilters, signal)
    if (signal.aborted || abortController !== requestController) return
    const rows = mergeSharedPlanRows([...userRows, ...organizationRows]).sort(compareSubscriptionRows)
    const offset = (pagination.page - 1) * pagination.page_size
    subscriptions.value = rows.slice(offset, offset + pagination.page_size)
    pagination.total = rows.length
    pagination.pages = Math.ceil(rows.length / pagination.page_size)
    const visibleIds = new Set(subscriptions.value.map((subscription) => subscription.id))
    setSelectedIds(selectedIds.value.filter((id) => visibleIds.has(id)))
  } catch (error: any) {
    if (signal.aborted || error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') {
      return
    }
    appStore.showError(t('admin.subscriptions.failedToLoad'))
    console.error('Error loading subscriptions:', error)
  } finally {
    if (abortController === requestController) {
      loading.value = false
      abortController = null
    }
  }
}

async function loadAllUserSubscriptionRows(filtersForRequest: Record<string, any>, signal: AbortSignal): Promise<AdminSubscriptionRow[]> {
  const rows: AdminSubscriptionRow[] = []
  let page = 1
  let pages = 1
  do {
    const response = await adminAPI.subscriptions.list(page, 100, {
      ...filtersForRequest,
      user_id: filters.user_id || undefined,
    }, { signal })
    rows.push(...response.items.map(item => ({ ...item, subject_type: 'user' as const })))
    pages = response.pages
    page += 1
  } while (page <= pages && !signal.aborted)
  return rows
}

async function loadAllOrganizationSubscriptionRows(filtersForRequest: Record<string, any>, signal: AbortSignal): Promise<AdminSubscriptionRow[]> {
  const rows: AdminSubscriptionRow[] = []
  let page = 1
  let pages = 1
  do {
    const response = await organizationAPI.listAdminOrganizationSubscriptions({ page, page_size: 100, ...filtersForRequest }, signal)
    rows.push(...response.items.map(organizationSubscriptionRow))
    pages = response.pages
    page += 1
  } while (page <= pages && !signal.aborted)
  return rows
}

function compareSubscriptionRows(left: AdminSubscriptionRow, right: AdminSubscriptionRow): number {
  const key = sortState.sort_by === 'expires_at' ? 'expires_at' : sortState.sort_by === 'status' ? 'status' : 'created_at'
  const leftValue = String(left[key] || '')
  const rightValue = String(right[key] || '')
  const comparison = leftValue.localeCompare(rightValue)
  return sortState.sort_order === 'asc' ? comparison : -comparison
}

function organizationSubscriptionRow(item: OrganizationSubscription): AdminSubscriptionRow {
  return {
    id: item.id,
    user_id: 0,
    group_id: item.group_id,
    subject_type: 'organization',
    organization: { id: item.organization_id, name: item.organization_name || `#${item.organization_id}`, company_id: item.company_id },
    status: item.status === 'cancelled' ? 'revoked' : item.status,
    starts_at: item.starts_at,
    expires_at: item.expires_at,
    daily_usage_usd: Number(item.daily_usage_usd),
    weekly_usage_usd: Number(item.weekly_usage_usd),
    monthly_usage_usd: Number(item.monthly_usage_usd),
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: null,
    created_at: item.created_at,
    updated_at: item.created_at,
    group: {
      id: item.group_id,
      name: item.group_name,
      platform: item.platform,
      subscription_type: item.subscription_type,
      daily_limit_usd: item.daily_limit_usd ? Number(item.daily_limit_usd) : null,
      weekly_limit_usd: item.weekly_limit_usd ? Number(item.weekly_limit_usd) : null,
      monthly_limit_usd: item.monthly_limit_usd ? Number(item.monthly_limit_usd) : null,
    } as Group,
    plan_id: item.plan_id,
    plan_name: item.plan_name,
    plan_daily_limit_usd: item.plan_daily_limit_usd ? Number(item.plan_daily_limit_usd) : null,
    plan_weekly_limit_usd: item.plan_weekly_limit_usd ? Number(item.plan_weekly_limit_usd) : null,
    plan_monthly_limit_usd: item.plan_monthly_limit_usd ? Number(item.plan_monthly_limit_usd) : null,
    group_ids: [item.group_id],
    group_names: [item.group_name],
    group_limits: [{
      group_id: item.group_id,
      name: item.group_name,
      daily_limit_usd: item.daily_limit_usd ? Number(item.daily_limit_usd) : null,
      weekly_limit_usd: item.weekly_limit_usd ? Number(item.weekly_limit_usd) : null,
      monthly_limit_usd: item.monthly_limit_usd ? Number(item.monthly_limit_usd) : null,
    }],
    group_usages: [{
      group_id: item.group_id,
      daily_usage_usd: Number(item.group_daily_usage_usd ?? item.daily_usage_usd),
      weekly_usage_usd: Number(item.group_weekly_usage_usd ?? item.weekly_usage_usd),
      monthly_usage_usd: Number(item.group_monthly_usage_usd ?? item.monthly_usage_usd),
    }],
  }
}

function hasPositivePlanLimit(row: AdminSubscriptionRow): boolean {
  return [row.plan_daily_limit_usd, row.plan_weekly_limit_usd, row.plan_monthly_limit_usd]
    .some((limit) => typeof limit === 'number' && limit > 0)
}

/**
 * Enterprise plan assignment creates one bindable row per covered group. The
 * admin table should show one shared quota block for that plan and list each
 * group's own usage underneath it. Plans without any quota remain separate
 * rows because their group counters and limits are independent.
 */
function mergeSharedPlanRows(rows: AdminSubscriptionRow[]): AdminSubscriptionRow[] {
  const merged: AdminSubscriptionRow[] = []
  const planRows = new Map<string, AdminSubscriptionRow>()

  for (const row of rows) {
    if ((row.subject_type !== 'organization' && row.subject_type !== 'user') || row.plan_id == null || !hasPositivePlanLimit(row)) {
      merged.push(row)
      continue
    }

    const subjectKey = row.subject_type === 'organization'
      ? `organization:${row.organization?.id ?? 0}`
      : `user:${row.user_id}`
    const key = `${subjectKey}:${row.plan_id}`
    const existing = planRows.get(key)
    if (!existing) {
      const first = {
        ...row,
        shared_subscription_ids: [row.id],
        group_ids: [...(row.group_ids || [])],
        group_names: [...(row.group_names || [])],
        group_limits: [...(row.group_limits || [])],
        group_usages: [...(row.group_usages || [])],
      }
      planRows.set(key, first)
      merged.push(first)
      continue
    }

    existing.shared_subscription_ids = [
      ...(existing.shared_subscription_ids || [existing.id]),
      row.id,
    ]
    for (const groupID of row.group_ids || [row.group_id]) {
      if (!existing.group_ids?.includes(groupID)) existing.group_ids?.push(groupID)
    }
    for (const name of row.group_names || []) {
      if (!existing.group_names?.includes(name)) existing.group_names?.push(name)
    }
    for (const limit of row.group_limits || []) {
      if (!existing.group_limits?.some((item) => item.group_id === limit.group_id)) {
        existing.group_limits?.push(limit)
      }
    }
    for (const usage of row.group_usages || []) {
      if (!existing.group_usages?.some((item) => item.group_id === usage.group_id)) {
        existing.group_usages?.push(usage)
      }
    }
  }

  return merged
}

const loadGroups = async () => {
  try {
    groups.value = await adminAPI.groups.getAll()
  } catch (error) {
    console.error('Error loading groups:', error)
  }
}

// Toolbar user filter search with debounce
const debounceSearchFilterUsers = () => {
  if (filterUserSearchTimeout) {
    clearTimeout(filterUserSearchTimeout)
  }
  filterUserSearchTimeout = setTimeout(searchFilterUsers, 300)
}

const searchFilterUsers = async () => {
  const keyword = filterUserKeyword.value.trim()

  // Clear active user filter if user modified the search keyword
  if (selectedFilterUser.value && keyword !== selectedFilterUser.value.email) {
    selectedFilterUser.value = null
    filters.user_id = null
    applyFilters()
  }

  if (!keyword) {
    filterUserResults.value = []
    return
  }

  filterUserLoading.value = true
  try {
    filterUserResults.value = await adminAPI.usage.searchUsers(keyword)
  } catch (error) {
    console.error('Failed to search users:', error)
    filterUserResults.value = []
  } finally {
    filterUserLoading.value = false
  }
}

const selectFilterUser = (user: SimpleUser) => {
  selectedFilterUser.value = user
  filterUserKeyword.value = user.email
  showFilterUserDropdown.value = false
  filters.user_id = user.id
  applyFilters()
}

const clearFilterUser = () => {
  selectedFilterUser.value = null
  filterUserKeyword.value = ''
  filterUserResults.value = []
  showFilterUserDropdown.value = false
  filters.user_id = null
  applyFilters()
}

// User search with debounce
const debounceSearchUsers = () => {
  // Invalidate the assignment target before the debounced search runs.
  if (selectedUser.value && userSearchKeyword.value.trim() !== selectedUser.value.email) {
    selectedUser.value = null
    assignForm.user_id = null
  }
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
  }
  userSearchTimeout = setTimeout(searchUsers, 300)
}

const searchUsers = async () => {
  const keyword = userSearchKeyword.value.trim()

  if (!keyword) {
    userSearchResults.value = []
    return
  }

  userSearchLoading.value = true
  try {
    const result = await adminAPI.users.list(1, 30, {
      search: keyword, sort_by: 'email', sort_order: 'asc'
    })
    userSearchResults.value = result.items
  } catch (error) {
    console.error('Failed to search users:', error)
    userSearchResults.value = []
  } finally {
    userSearchLoading.value = false
  }
}

const selectUser = (user: AdminUser) => {
  if (submitting.value) return
  if (batchAssignEnabled.value) {
    if (assignUsers.value.length < 100 && !assignUsers.value.some((selected) => selected.id === user.id)) {
      assignUsers.value = [...assignUsers.value, user]
    }
    userSearchKeyword.value = ''
    userSearchResults.value = []
    showUserDropdown.value = false
    return
  }
  selectedUser.value = user
  userSearchKeyword.value = user.email
  showUserDropdown.value = false
  assignForm.user_id = user.id
}

const clearUserSelection = () => {
  selectedUser.value = null
  userSearchKeyword.value = ''
  userSearchResults.value = []
  assignForm.user_id = null
  assignForm.organization_id = null
}

const resetAssignUsers = () => {
  clearUserSelection()
  assignUsers.value = []
  batchAssignResult.value = null
}

const handlePageChange = (page: number) => {
  clearSelection()
  pagination.page = page
  loadSubscriptions()
}

const handlePageSizeChange = (pageSize: number) => {
  clearSelection()
  pagination.page_size = pageSize
  pagination.page = 1
  loadSubscriptions()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  clearSelection()
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadSubscriptions()
}

const closeAssignModal = () => {
  if (submitting.value) return
  showAssignModal.value = false
  batchAssignEnabled.value = false
  assignUsers.value = []
  batchAssignResult.value = null
  assignForm.user_id = null
  assignForm.group_id = null
  assignForm.plan_id = null
  assignForm.validity_days = 30
  // Clear user search state
  selectedUser.value = null
  userSearchKeyword.value = ''
  userSearchResults.value = []
  showUserDropdown.value = false
  assignTarget.value = 'user'
  assignSource.value = 'group'
}

const handleAssignSubscription = async () => {
  if (submitting.value) return
  if ((assignTarget.value === 'user' && (batchAssignEnabled.value ? assignUsers.value.length === 0 : !assignForm.user_id))) {
    appStore.showError(t('admin.subscriptions.pleaseSelectUser'))
    return
  }
  if (assignTarget.value === 'organization' && !assignForm.organization_id) {
    appStore.showError(t('admin.subscriptions.pleaseSelectEnterprise'))
    return
  }
  const byPlan = assignSource.value === 'plan'
  if (byPlan && !assignForm.plan_id) {
    appStore.showError(t('admin.subscriptions.pleaseSelectPlan'))
    return
  }
  if (!byPlan && !assignForm.group_id) {
    appStore.showError(t('admin.subscriptions.pleaseSelectGroup'))
    return
  }
  if (!Number.isInteger(assignForm.validity_days) || assignForm.validity_days < 1 || assignForm.validity_days > 36500) {
    appStore.showError(t('admin.subscriptions.validityDaysRequired'))
    return
  }

  submitting.value = true
  try {
    if (assignTarget.value === 'organization') {
      await organizationAPI.assignOrganizationSubscription(
        assignForm.organization_id!,
        byPlan ? { plan_id: assignForm.plan_id! } : { group_id: assignForm.group_id! },
        assignForm.validity_days
      )
    } else {
      // group_id 与 plan_id 必须互斥地发出：后端拒绝同时收到两者，因为各自带一套
      // 分组集合与有效期，偏向任何一方都会让另一方看起来也生效了。
      await adminAPI.subscriptions.assign({
        user_id: assignForm.user_id!,
        ...(byPlan ? { plan_id: assignForm.plan_id! } : { group_id: assignForm.group_id! }),
        validity_days: assignForm.validity_days
      })
    }
    appStore.showSuccess(t('admin.subscriptions.subscriptionAssigned'))
    submitting.value = false
    closeAssignModal()
    loadSubscriptions()
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.subscriptions.failedToAssign'))
    console.error('Error assigning subscription:', error)
  } finally {
    submitting.value = false
  }
}

const handleExtend = (subscription: AdminSubscriptionRow) => {
  extendingSubscription.value = subscription
  extendForm.days = 30
  showExtendModal.value = true
}

const closeExtendModal = () => {
  showExtendModal.value = false
  extendingSubscription.value = null
}

const handleExtendSubscription = async () => {
  if (!extendingSubscription.value) return

  // 前端验证：调整后的过期时间必须在未来
  if (extendingSubscription.value.expires_at) {
    const expiresAt = new Date(extendingSubscription.value.expires_at)
    const newExpiresAt = new Date(expiresAt.getTime() + extendForm.days * 24 * 60 * 60 * 1000)
    if (newExpiresAt <= new Date()) {
      appStore.showError(t('admin.subscriptions.adjustWouldExpire'))
      return
    }
  }

  submitting.value = true
  try {
    if (extendingSubscription.value.subject_type === 'organization') {
      for (const subscriptionID of sharedSubscriptionIds(extendingSubscription.value)) {
        await organizationAPI.extendAdminOrganizationSubscription(subscriptionID, extendForm.days)
      }
    } else if (extendingSubscription.value.shared_subscription_ids?.length) {
      for (const subscriptionID of sharedSubscriptionIds(extendingSubscription.value)) {
        await adminAPI.subscriptions.extend(subscriptionID, { days: extendForm.days })
      }
    } else {
      await adminAPI.subscriptions.extend(extendingSubscription.value.id, { days: extendForm.days })
    }
    appStore.showSuccess(t('admin.subscriptions.subscriptionAdjusted'))
    closeExtendModal()
    loadSubscriptions()
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.subscriptions.failedToAdjust'))
    console.error('Error adjusting subscription:', error)
  } finally {
    submitting.value = false
  }
}

const handleRevoke = (subscription: AdminSubscriptionRow) => {
  revokingSubscription.value = subscription
  showRevokeDialog.value = true
}

const confirmRevoke = async () => {
  if (!revokingSubscription.value) return

  try {
    if (revokingSubscription.value.subject_type === 'organization') {
      for (const subscriptionID of sharedSubscriptionIds(revokingSubscription.value)) {
        await organizationAPI.revokeAdminOrganizationSubscription(subscriptionID)
      }
    } else if (revokingSubscription.value.shared_subscription_ids?.length) {
      for (const subscriptionID of sharedSubscriptionIds(revokingSubscription.value)) {
        await adminAPI.subscriptions.revoke(subscriptionID)
      }
    } else {
      await adminAPI.subscriptions.revoke(revokingSubscription.value.id)
    }
    appStore.showSuccess(t('admin.subscriptions.subscriptionRevoked'))
    showRevokeDialog.value = false
    revokingSubscription.value = null
    loadSubscriptions()
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.subscriptions.failedToRevoke'))
    console.error('Error revoking subscription:', error)
  }
}

const handleRestore = (subscription: UserSubscription) => {
  restoringSubscription.value = subscription
  showRestoreDialog.value = true
}

const confirmRestore = async () => {
  if (!restoringSubscription.value) return

  try {
    await adminAPI.subscriptions.restore(restoringSubscription.value.id)
    appStore.showSuccess(t('admin.subscriptions.subscriptionRestored'))
    showRestoreDialog.value = false
    restoringSubscription.value = null
    loadSubscriptions()
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.subscriptions.failedToRestore'))
    console.error('Error restoring subscription:', error)
  }
}

const handleResetQuota = (subscription: AdminSubscriptionRow) => {
  resettingSubscription.value = subscription
  showResetQuotaConfirm.value = true
}

const confirmResetQuota = async () => {
  if (!resettingSubscription.value) return
  if (resettingQuota.value) return
  resettingQuota.value = true
  try {
    if (resettingSubscription.value.subject_type === 'organization') {
      await organizationAPI.resetAdminOrganizationSubscriptionQuota(resettingSubscription.value.id)
      // One enterprise reset clears the shared package pool and all bound rows.
    } else if (resettingSubscription.value.shared_subscription_ids?.length) {
      for (const subscriptionID of sharedSubscriptionIds(resettingSubscription.value)) {
        await adminAPI.subscriptions.resetQuota(subscriptionID, { daily: true, weekly: true, monthly: true })
      }
    } else {
      await adminAPI.subscriptions.resetQuota(resettingSubscription.value.id, { daily: true, weekly: true, monthly: true })
    }
    appStore.showSuccess(t('admin.subscriptions.quotaResetSuccess'))
    showResetQuotaConfirm.value = false
    resettingSubscription.value = null
    await loadSubscriptions()
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.subscriptions.failedToResetQuota'))
    console.error('Error resetting quota:', error)
  } finally {
    resettingQuota.value = false
  }
}

// Helper functions
const subscriptionSubjectName = (subscription: AdminSubscriptionRow | null): string => {
  if (!subscription) return ''
  return subscription.subject_type === 'organization'
    ? subscription.organization?.name || `#${subscription.organization?.id || subscription.id}`
    : subscription.user?.email || `#${subscription.user_id}`
}

const sharedSubscriptionIds = (subscription: AdminSubscriptionRow): number[] =>
  subscription.shared_subscription_ids?.length
    ? subscription.shared_subscription_ids
    : [subscription.id]

const getDaysRemaining = (expiresAt: string): number | null => {
  const now = new Date()
  const expires = new Date(expiresAt)
  const diff = expires.getTime() - now.getTime()
  if (diff < 0) return null
  return Math.ceil(diff / (1000 * 60 * 60 * 24))
}

const formatRemainingExpiry = (expiresAt: string): string | null => {
  const duration = getRemainingExpiryDuration(expiresAt)
  if (!duration) return null
  if (duration.unit === 'days') {
    return t('admin.subscriptions.daysRemaining', { days: duration.days })
  }
  if (duration.hours) {
    return t('admin.subscriptions.hoursMinutesRemaining', {
      hours: duration.hours,
      minutes: duration.minutes
    })
  }
  return t('admin.subscriptions.minutesRemaining', { minutes: duration.minutes })
}

const isExpiringSoon = (expiresAt: string): boolean => {
  const days = getDaysRemaining(expiresAt)
  return days !== null && days <= 7
}

const getProgressWidth = (used: number | null | undefined, limit: number | null): string => {
  if (!limit || limit === 0) return '0%'
  const usedValue = used ?? 0
  const percentage = Math.min((usedValue / limit) * 100, 100)
  return `${percentage}%`
}

const getProgressClass = (used: number | null | undefined, limit: number | null): string => {
  if (!limit || limit === 0) return 'bg-gray-400'
  const usedValue = used ?? 0
  const percentage = (usedValue / limit) * 100
  if (percentage >= 90) return 'bg-red-500'
  if (percentage >= 70) return 'bg-orange-500'
  return 'bg-green-500'
}

const formatResetDuration = (parts: RemainingDurationParts): string => {
  if (parts.days > 0) {
    return t('admin.subscriptions.resetInDaysHours', { days: parts.days, hours: parts.hours })
  }

  if (parts.hours > 0) {
    return t('admin.subscriptions.resetInHoursMinutes', { hours: parts.hours, minutes: parts.minutes })
  }

  return t('admin.subscriptions.resetInMinutes', { minutes: parts.minutes })
}

const formatQuotaEndDuration = (parts: RemainingDurationParts): string => {
  if (parts.days > 0) {
    return t('admin.subscriptions.quotaEndsInDaysHours', { days: parts.days, hours: parts.hours })
  }

  if (parts.hours > 0) {
    return t('admin.subscriptions.quotaEndsInHoursMinutes', { hours: parts.hours, minutes: parts.minutes })
  }

  return t('admin.subscriptions.quotaEndsInMinutes', { minutes: parts.minutes })
}

const formatDailyUsageWindow = (subscription: UserSubscription): string => {
  if (isOneTimeDailyQuota(subscription) && subscription.expires_at) {
    const parts = getRemainingDurationParts(subscription.expires_at)
    return parts ? formatQuotaEndDuration(parts) : t('admin.subscriptions.windowNotActive')
  }

  return formatResetTime(subscription.daily_window_start, 'daily')
}

// Format reset time based on window start and period type
const formatResetTime = (windowStart: string | null, period: 'daily' | 'weekly' | 'monthly'): string => {
  if (!windowStart) return t('admin.subscriptions.windowNotActive')

  const start = new Date(windowStart)
  const now = new Date()

  // Calculate reset time based on period
  let resetTime: Date
  switch (period) {
    case 'daily':
      resetTime = new Date(start.getTime() + 24 * 60 * 60 * 1000)
      break
    case 'weekly':
      resetTime = new Date(start.getTime() + 7 * 24 * 60 * 60 * 1000)
      break
    case 'monthly':
      resetTime = new Date(start.getTime() + 30 * 24 * 60 * 60 * 1000)
      break
  }

  const parts = getRemainingDurationParts(resetTime, now)

  return parts ? formatResetDuration(parts) : t('admin.subscriptions.windowNotActive')
}

const usageWindowResetText = (row: AdminSubscriptionRow, window: UsageWindow): string => {
  if (window === 'daily') {
    return row.daily_window_start ? formatDailyUsageWindow(row) : ''
  }

  const windowStart = window === 'weekly' ? row.weekly_window_start : row.monthly_window_start
  return windowStart ? formatResetTime(windowStart, window) : ''
}

// Handle click outside to close dropdowns
const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (!target.closest('[data-assign-user-search]')) showUserDropdown.value = false
  if (!target.closest('[data-filter-user-search]')) showFilterUserDropdown.value = false
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(target)) {
    showColumnDropdown.value = false
  }
}

onMounted(() => {
  loadUserColumnMode()
  loadSavedColumns()
  loadSubscriptions()
  loadGroups()
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  if (filterUserSearchTimeout) {
    clearTimeout(filterUserSearchTimeout)
  }
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
  }
})
</script>
