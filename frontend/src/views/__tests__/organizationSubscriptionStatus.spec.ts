import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import OrganizationConsoleView from '@/views/user/OrganizationConsoleView.vue'

const api = vi.hoisted(() => ({
  getContext: vi.fn(),
  listSubscriptions: vi.fn(),
  listSubscriptionGroups: vi.fn().mockResolvedValue([]),
}))
vi.mock('@/api', () => ({ organizationAPI: api }))
vi.mock('@/api/plaza', () => ({ plazaAPI: { listPlans: vi.fn().mockResolvedValue({ cards: [] }) } }))
vi.mock('@/stores', () => ({ useAuthStore: () => ({ user: { id: 99 }, refreshUser: vi.fn() }) }))
vi.mock('vue-chartjs', () => ({ Doughnut: { template: '<div />' }, Line: { template: '<div />' } }))

const router = createRouter({
  history: createMemoryHistory(),
  routes: [{ path: '/organization', component: { template: '<div />' } }],
})
const mountOptions = {
  global: {
    plugins: [
      createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }),
      createPinia(),
      router,
    ],
    stubs: { AppLayout: { template: '<div><slot /></div>' } },
  },
}

describe('enterprise subscription status', () => {
  it('shows expired enterprise subscriptions by expiry time and updates while the page stays open', async () => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-10-17T08:00:00Z'))
    let wrapper: ReturnType<typeof mount> | undefined
    try {
      await router.replace('/organization?tab=subscriptions')
      api.getContext.mockResolvedValue({
        organization: { organization_id: 1, account_id: '1719905235756637', company_name: 'Example', organization_status: 'active', membership_status: 'active', role: 'owner', actions: [] },
        finance: { balance_source: 'self', available: '100', frozen: '0', total: '100' },
      })
      const subscription = {
        organization_id: 1, group_id: 7, platform: 'openai', subscription_type: 'subscription',
        rate_multiplier: 0.2, starts_at: '2026-01-01T00:00:00Z',
        expires_at: '2026-10-17T15:59:59+08:00', status: 'active',
        daily_usage_usd: '0', weekly_usage_usd: '0', monthly_usage_usd: '0',
        assigned_at: '2026-01-01T00:00:00Z', created_at: '2026-01-01T00:00:00Z',
      }
      api.listSubscriptions.mockResolvedValue([
        { ...subscription, id: 1, group_name: 'Expired single group' },
        { ...subscription, id: 2, group_name: 'Shared route A', plan_id: 10, plan_name: 'Expired shared plan', plan_daily_limit_usd: '10' },
        { ...subscription, id: 3, group_name: 'Shared route B', group_id: 8, plan_id: 10, plan_name: 'Expired shared plan', plan_daily_limit_usd: '10' },
        { ...subscription, id: 4, group_name: 'Cancelled plan', status: 'cancelled' },
        { ...subscription, id: 5, group_name: 'Expires now', expires_at: '2026-10-17T08:00:00Z' },
        { ...subscription, id: 6, group_name: 'Expires shortly', expires_at: '2026-10-17T08:00:01Z' },
      ])
      wrapper = mount(OrganizationConsoleView, mountOptions)
      await flushPromises()
      const statusCell = (name: string) => {
        const row = wrapper!.findAll('tbody tr').find(item => item.text().includes(name))
        expect(row).toBeDefined()
        return row!.findAll('td')[3].get('span')
      }
      for (const name of ['Expired single group', 'Expired shared plan', 'Expires now']) {
        expect(statusCell(name).text()).toBe('organization.subscriptions.statuses.expired')
        expect(statusCell(name).classes()).toContain('bg-amber-100')
      }
      expect(statusCell('Cancelled plan').text()).toBe('organization.subscriptions.statuses.cancelled')
      expect(statusCell('Expires shortly').text()).toBe('organization.subscriptions.statuses.active')
      expect(statusCell('Expires shortly').classes()).toContain('bg-green-100')

      await vi.advanceTimersByTimeAsync(1000)
      expect(statusCell('Expires shortly').text()).toBe('organization.subscriptions.statuses.expired')
      expect(statusCell('Expires shortly').classes()).toContain('bg-amber-100')
    } finally {
      wrapper?.unmount()
      vi.useRealTimers()
    }
  })

})
