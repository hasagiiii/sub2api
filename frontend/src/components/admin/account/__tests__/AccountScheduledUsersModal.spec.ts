import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountScheduledUsersModal from '../AccountScheduledUsersModal.vue'
import type { Account, AccountScheduledUsersResponse } from '@/types'

const { getScheduledUsers, evictScheduledUser, showError, showSuccess } = vi.hoisted(() => ({
  getScheduledUsers: vi.fn(),
  evictScheduledUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { accounts: { getScheduledUsers, evictScheduledUser } }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key)
    })
  }
})

const response = (overrides: Partial<AccountScheduledUsersResponse> = {}): AccountScheduledUsersResponse => ({
  account_id: 7,
  max_users: 2,
  bound_count: 2,
  evict_cooldown_seconds: 3600,
  users: [
    { user_id: 11, email: 'alice@example.com', username: 'alice', bound: true, bound_at: '2026-10-01T08:00:00Z', sessions: 5, requests: 20, last_active_at: '2026-10-11T08:00:00Z' },
    { user_id: 12, email: '', username: '', bound: true, bound_at: '2026-10-02T08:00:00Z', sessions: 0, requests: 0, last_active_at: null },
    { user_id: 13, email: 'carol@example.com', username: '', bound: false, evicted_until: '2026-10-11T09:00:00Z', sessions: 2, requests: 3, last_active_at: '2026-10-10T08:00:00Z' }
  ],
  ...overrides
})

const ConfirmDialogStub = {
  props: ['show', 'message'],
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show" class="confirm-dialog">{{ message }}<button class="confirm-btn" @click="$emit(\'confirm\')" /></div>'
}

const mountOpened = async () => {
  const wrapper = mount(AccountScheduledUsersModal, {
    props: { show: false, account: { id: 7, name: 'acc' } as Account },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /></div>' },
        ConfirmDialog: ConfirmDialogStub,
        LoadingSpinner: true,
        Icon: true
      }
    }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('AccountScheduledUsersModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows slot usage and users with their conversations', async () => {
    getScheduledUsers.mockResolvedValue(response())
    const wrapper = await mountOpened()

    expect(getScheduledUsers).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-test="scheduled-users-slots"]').text()).toBe('2 / 2')
    const rows = wrapper.findAll('[data-test="scheduled-user-row"]')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain('alice@example.com')
    expect(rows[0].text()).toContain('5')
    expect(rows[0].text()).toContain('admin.accounts.scheduledUsers.bound')
    expect(rows[1].text()).toContain('admin.accounts.scheduledUsers.unknownUser:{"id":12}')
    expect(rows[2].text()).toContain('admin.accounts.scheduledUsers.evicted')
    expect(wrapper.findAll('[data-test="scheduled-user-evict"]')).toHaveLength(2)
  })

  it('shows unlimited when max users is not configured and empty state', async () => {
    getScheduledUsers.mockResolvedValue(response({ max_users: 0, bound_count: 0, users: [] }))
    const wrapper = await mountOpened()

    expect(wrapper.get('[data-test="scheduled-users-slots"]').text()).toBe('0 / admin.accounts.scheduledUsers.unlimited')
    expect(wrapper.find('[data-test="scheduled-users-empty"]').exists()).toBe(true)
  })

  it('evicts a bound user after confirmation and reloads', async () => {
    getScheduledUsers.mockResolvedValueOnce(response())
    getScheduledUsers.mockResolvedValueOnce(response({
      bound_count: 1,
      users: [
        { user_id: 11, email: 'alice@example.com', username: 'alice', bound: false, evicted_until: '2026-10-11T09:00:00Z', sessions: 5, requests: 20, last_active_at: null },
        { user_id: 12, email: '', username: '', bound: true, sessions: 0, requests: 0, last_active_at: null }
      ]
    }))
    evictScheduledUser.mockResolvedValue({ removed: true })
    const wrapper = await mountOpened()

    await wrapper.findAll('[data-test="scheduled-user-evict"]')[0].trigger('click')
    const dialog = wrapper.get('.confirm-dialog')
    expect(dialog.text()).toContain('"user":"alice@example.com"')
    expect(dialog.text()).toContain('"minutes":60')
    await dialog.get('.confirm-btn').trigger('click')
    await flushPromises()

    expect(evictScheduledUser).toHaveBeenCalledWith(7, 11)
    expect(showSuccess).toHaveBeenCalled()
    expect(getScheduledUsers).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="scheduled-users-slots"]').text()).toBe('1 / 2')
    expect(wrapper.findAll('[data-test="scheduled-user-evict"]')).toHaveLength(1)
  })

  it('reports evict failures without reloading', async () => {
    getScheduledUsers.mockResolvedValue(response())
    evictScheduledUser.mockRejectedValue(new Error('boom'))
    const wrapper = await mountOpened()

    await wrapper.findAll('[data-test="scheduled-user-evict"]')[0].trigger('click')
    await wrapper.get('.confirm-dialog .confirm-btn').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(getScheduledUsers).toHaveBeenCalledTimes(1)
  })
})
