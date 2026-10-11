import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountStatsModal from '../AccountStatsModal.vue'
import type { Account, AccountUsageStatsResponse } from '@/types'

const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }))

vi.mock('@/api/admin', () => ({
  adminAPI: { accounts: { getStats } }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="trend-chart">{{ JSON.stringify(data.datasets.map((d) => [d.label, d.data])) }}</div>'
  }
}))

const statsResponse: AccountUsageStatsResponse = {
  history: [
    { date: '2026-10-10', label: '10/10', requests: 5, sessions: 2, tokens: 100, cost: 1, actual_cost: 1, user_cost: 1 },
    { date: '2026-10-11', label: '10/11', requests: 3, sessions: 1, tokens: 50, cost: 0.5, actual_cost: 0.5, user_cost: 0.5 }
  ],
  summary: {
    days: 30,
    actual_days_used: 2,
    total_cost: 1.5,
    total_user_cost: 1.5,
    total_standard_cost: 1.5,
    total_requests: 8,
    total_tokens: 150,
    total_sessions: 2,
    lifetime_sessions: 9,
    avg_daily_cost: 0.75,
    avg_daily_user_cost: 0.75,
    avg_daily_requests: 4,
    avg_daily_tokens: 75,
    avg_daily_sessions: 1.5,
    avg_duration_ms: 100,
    today: { date: '2026-10-11', cost: 0.5, user_cost: 0.5, requests: 3, sessions: 1, tokens: 50 },
    highest_cost_day: null,
    highest_request_day: null
  },
  models: [],
  endpoints: [],
  upstream_endpoints: [],
  user_sessions: [
    { user_id: 11, email: 'alice@example.com', username: 'alice', sessions: 2, lifetime_sessions: 6, last_active_at: '2026-10-11T08:00:00Z' },
    { user_id: 12, email: 'bob@example.com', username: '', sessions: 0, lifetime_sessions: 3, last_active_at: null }
  ]
}

const mountOpened = async (response: AccountUsageStatsResponse) => {
  getStats.mockResolvedValue(response)
  const wrapper = mount(AccountStatsModal, {
    props: { show: false, account: { id: 7, name: 'acc', status: 'active' } as Account },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        LoadingSpinner: true,
        ModelDistributionChart: true,
        EndpointDistributionChart: true,
        Icon: true
      }
    }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('AccountStatsModal', () => {
  it('shows session counts in summary, today overview and trend chart', async () => {
    const wrapper = await mountOpened(statsResponse)

    expect(getStats).toHaveBeenCalledWith(7, 30)
    const card = wrapper.get('[data-test="account-stats-sessions"]')
    expect(card.text()).toContain('admin.accounts.stats.totalSessions')
    expect(card.text()).toContain('2')
    expect(card.text()).toContain('admin.accounts.stats.avgDailySessions')
    expect(wrapper.get('[data-test="account-stats-lifetime-sessions"]').text()).toBe('admin.accounts.stats.lifetimeSessions: 9')
    expect(wrapper.text()).toContain('admin.accounts.stats.sessions')
    expect(wrapper.get('.trend-chart').text()).toContain('["admin.accounts.stats.sessions",[2,1]]')
  })

  it('lists per-user session counts', async () => {
    const wrapper = await mountOpened(statsResponse)
    const rows = wrapper.findAll('[data-test="account-stats-user-session-row"]')
    expect(rows).toHaveLength(2)

    const alice = rows[0].findAll('td').map((cell) => cell.text())
    expect(alice[0]).toContain('alice')
    expect(alice[0]).toContain('alice@example.com')
    expect(alice[1]).toBe('2')
    expect(alice[2]).toBe('6')
    expect(alice[3]).not.toBe('-')

    const bob = rows[1].findAll('td').map((cell) => cell.text())
    expect(bob[0]).toBe('bob@example.com')
    expect(bob[1]).toBe('0')
    expect(bob[2]).toBe('3')
    expect(bob[3]).toBe('-')
  })

  it('shows an empty state when no user has sessions', async () => {
    const wrapper = await mountOpened({ ...statsResponse, user_sessions: [] })
    const section = wrapper.get('[data-test="account-stats-user-sessions"]')
    expect(section.findAll('[data-test="account-stats-user-session-row"]')).toHaveLength(0)
    expect(section.text()).toContain('admin.accounts.stats.noUserSessions')
  })
})
