import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import SubscriptionsView from '../SubscriptionsView.vue'

const { listSubscriptions, getAllGroups, assignSubscription, getPlans, searchUsers, listUsers, listAdminOrganizationSubscriptions, listOrganizations } = vi.hoisted(() => ({
  listSubscriptions: vi.fn(),
  getAllGroups: vi.fn(),
  assignSubscription: vi.fn(),
  getPlans: vi.fn(),
  searchUsers: vi.fn(),
  listUsers: vi.fn(),
  listAdminOrganizationSubscriptions: vi.fn(),
  listOrganizations: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    subscriptions: { list: listSubscriptions, assign: assignSubscription },
    groups: { getAll: getAllGroups },
    usage: { searchUsers },
    users: { list: listUsers }
  }
}))

vi.mock('@/api/organization', () => ({
  organizationAPI: {
    listOrganizations,
    listAdminOrganizationSubscriptions
  }
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: { getPlans }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" data-row>
        <div data-group-cell><slot name="cell-group" :row="row" /></div>
        <div data-usage-cell><slot name="cell-usage" :row="row" /></div>
      </div>
    </div>
  `
}

/** 渲染插槽内容的 BaseDialog 桩，让分配表单可被驱动。 */
const BaseDialogStub = {
  template: '<div><slot /><slot name="footer" /></div>'
}

const SelectStub = defineComponent({
  props: {
    modelValue: { type: [Number, String], default: null },
    options: { type: Array as () => { value: number | string; label: string }[], default: () => [] }
  },
  emits: ['update:modelValue'],
  methods: {
    onChange(event: Event) {
      this.$emit('update:modelValue', Number((event.target as HTMLSelectElement).value))
    }
  },
  template: `
    <select :value="modelValue" @change="onChange">
      <option v-for="opt in options" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
    </select>
  `
})

const GroupBadgeStub = defineComponent({
  props: { name: { type: String, default: '' } },
  template: '<span data-group-badge>{{ name }}</span>'
})

const subscriptionRow = (overrides: Record<string, unknown> = {}) => ({
  id: 9,
  user_id: 42,
  group_id: 3,
  status: 'active',
  starts_at: '2026-01-01T00:00:00Z',
  expires_at: null,
  daily_usage_usd: 0,
  weekly_usage_usd: 0,
  monthly_usage_usd: 0,
  daily_window_start: null,
  weekly_window_start: null,
  monthly_window_start: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  user: { email: 'reader@example.com' },
  ...overrides
})

const mountView = () => mount(SubscriptionsView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
      DataTable: DataTableStub,
      RouterLink: { template: '<a><slot /></a>' },
      Pagination: true,
      BaseDialog: BaseDialogStub,
      ConfirmDialog: true,
      EmptyState: true,
      Select: SelectStub,
      GroupBadge: GroupBadgeStub,
      GroupOptionItem: true,
      Icon: true,
      Teleport: true
    }
  }
})

describe('admin subscription assignment by plan', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    listSubscriptions.mockResolvedValue({ items: [], total: 0, pages: 0 })
    listAdminOrganizationSubscriptions.mockResolvedValue({ items: [], total: 0, pages: 0 })
    listOrganizations.mockResolvedValue({ items: [], total: 0 })
    searchUsers.mockResolvedValue([{ id: 42, email: 'reader@example.com', username: 'Reader' }])
    listUsers.mockResolvedValue({ items: [{ id: 42, email: 'reader@example.com', username: 'Reader' }] })
    getAllGroups.mockResolvedValue([
      { id: 3, name: 'Alpha', platform: 'openai', status: 'active', subscription_type: 'subscription', rate_multiplier: 1 },
      { id: 4, name: 'Beta', platform: 'gemini', status: 'active', subscription_type: 'subscription', rate_multiplier: 1 }
    ])
    assignSubscription.mockResolvedValue({})
    getPlans.mockResolvedValue({
      data: [{
        id: 77,
        name: 'Bundle',
        group_id: 3,
        group_ids: [3, 4],
        price: 10,
        validity_days: 90,
        validity_unit: 'days',
        features: '',
        for_sale: true,
        sort_order: 0,
        description: ''
      }]
    })
  })

  const openAssignDialog = async (wrapper: ReturnType<typeof mountView>) => {
    const assignButton = wrapper.findAll('button').find(button => button.text().includes('assignSubscription'))
    await assignButton!.trigger('click')
    await flushPromises()

    // 选中目标用户：搜索框防抖 300ms 后才真正发起查询，等待后从下拉里点选。
    const form = wrapper.find('#assign-subscription-form')
    const userInput = form.find('input[type="text"]')
    await userInput.trigger('focus')
    await userInput.setValue('reader@example.com')
    await new Promise(resolve => setTimeout(resolve, 350))
    await flushPromises()
    const userOption = wrapper.findAll('button').find(button => button.text() === 'reader@example.com#42')
    await userOption!.trigger('click')
  }

  // 按套餐分配必须只发 plan_id：后端拒绝同时收到 group_id 与 plan_id，
  // 因为两者各带一套分组集合与有效期。
  it('submits only the plan id when assigning from a plan', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openAssignDialog(wrapper)

    const byPlanButton = wrapper.findAll('button').find(button => button.text().includes('byPlan'))
    await byPlanButton!.trigger('click')

    const planSelect = wrapper.find('#assign-subscription-form select')
    await planSelect.setValue('77')
    await wrapper.find('#assign-subscription-form').trigger('submit')
    await flushPromises()

    expect(assignSubscription).toHaveBeenCalledTimes(1)
    const payload = assignSubscription.mock.calls[0][0]
    expect(payload.plan_id).toBe(77)
    expect(payload.group_id).toBeUndefined()
    // 选中套餐后带出套餐自身的时长，与用户自行购买拿到的期限一致。
    expect(payload.validity_days).toBe(90)
  })

  it('submits only the group id when assigning from a group', async () => {
    const wrapper = mountView()
    await flushPromises()
    await openAssignDialog(wrapper)

    const groupSelect = wrapper.find('#assign-subscription-form select')
    await groupSelect.setValue('4')
    await wrapper.find('#assign-subscription-form').trigger('submit')
    await flushPromises()

    expect(assignSubscription).toHaveBeenCalledTimes(1)
    const payload = assignSubscription.mock.calls[0][0]
    expect(payload.group_id).toBe(4)
    expect(payload.plan_id).toBeUndefined()
  })

  // 套餐订阅是一条覆盖多个分组的订阅，列表只显示主分组会让管理员以为它只作用于
  // 一个分组。
  it('lists every group a subscription covers', async () => {
    listSubscriptions.mockResolvedValue({
      items: [subscriptionRow({ plan_id: 77, group_ids: [3, 4] })],
      total: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    const badges = wrapper.find('[data-group-cell]').findAll('[data-group-badge]')
    expect(badges.map(badge => badge.text())).toEqual(['Alpha', 'Beta'])
  })

  // 用量分母必须用订阅的生效限额。套餐订阅的额度来自套餐，按主分组的限额算会显示
  // 错误的进度——分组没配限额时甚至会显示成"无限制"。
  it('uses the subscription effective limit as the usage denominator', async () => {
    getAllGroups.mockResolvedValue([
      { id: 3, name: 'Alpha', platform: 'openai', status: 'active', subscription_type: 'subscription', rate_multiplier: 1, daily_limit_usd: null }
    ])
    listSubscriptions.mockResolvedValue({
      items: [subscriptionRow({
        plan_id: 77,
        group_ids: [3],
        daily_usage_usd: 2.5,
        daily_limit_usd: 20,
        group: { id: 3, name: 'Alpha', platform: 'openai', daily_limit_usd: null }
      })],
      total: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    const usage = wrapper.find('[data-usage-cell]').text()
    expect(usage).toContain('$20.00')
    expect(usage).not.toContain('unlimited')
  })
})
