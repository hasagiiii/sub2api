import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import OrganizationConsoleView from '@/views/user/OrganizationConsoleView.vue'
import CompanyApplicationsView from '@/views/admin/CompanyApplicationsView.vue'

const memberAPI = vi.hoisted(() => ({
  getContext: vi.fn(), listMembers: vi.fn(), listOrganizations: vi.fn(), listApplications: vi.fn(),
  setOrganizationStatus: vi.fn(), getOrganization: vi.fn(), requestNameChange: vi.fn(),
}))
const adminConsoleAPI = vi.hoisted(() => ({
  getContext: vi.fn(), listMembers: vi.fn(), listPolicies: vi.fn(), transferCompanyBalance: vi.fn(),
  listSubscriptions: vi.fn(), listSubscriptionGroups: vi.fn(),
}))
const createAdminOrganizationConsoleAPI = vi.hoisted(() => vi.fn(() => adminConsoleAPI))
const plaza = vi.hoisted(() => ({ listPlans: vi.fn() }))
const auth = vi.hoisted(() => ({ user: { id: 1 }, refreshUser: vi.fn() }))

vi.mock('@/api', () => ({ organizationAPI: memberAPI }))
vi.mock('@/api/organization', () => ({ createAdminOrganizationConsoleAPI }))
vi.mock('@/api/plaza', () => ({ plazaAPI: plaza }))
vi.mock('@/stores', () => ({ useAuthStore: () => auth }))
vi.mock('vue-chartjs', () => ({ Doughnut: { template: '<div />' }, Line: { template: '<div />' } }))

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false })
const view = { template: '<div />' }
const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/admin/company-applications', component: view },
    { path: '/admin/organizations/:organizationId/:tab?', component: view },
    { path: '/organization/:tab?', component: view },
  ],
})
const mountOptions = {
  global: {
    plugins: [i18n, createPinia(), router],
    stubs: { AppLayout: { template: '<div><slot /></div>' }, Doughnut: true },
  },
}
const ownerContext = {
  organization: { organization_id: 5, account_id: 'acct', company_id: 'c5', company_name: 'Acme', organization_status: 'active', membership_status: 'active', role: 'owner', actions: [] },
  finance: { balance_source: 'self', available: '0', frozen: '0', total: '0', company_available: '100', company_frozen: '0', company_total: '100' },
}

describe('system administrator enterprise management', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    adminConsoleAPI.getContext.mockResolvedValue(ownerContext)
    adminConsoleAPI.listMembers.mockResolvedValue({ items: [], member_limit: 20, used_slots: 0 })
    adminConsoleAPI.listPolicies.mockResolvedValue([])
    adminConsoleAPI.listSubscriptions.mockResolvedValue([])
    adminConsoleAPI.listSubscriptionGroups.mockResolvedValue([])
    plaza.listPlans.mockResolvedValue({ cards: [] })
  })

  it('opens the console for an active organization from the organization list', async () => {
    await router.replace('/admin/company-applications?queue=organizations')
    memberAPI.listOrganizations.mockResolvedValue({ items: [
      { id: 5, account_id: 'acct', company_id: 'c5', name: 'Acme', status: 'active', owner_user_id: 9, member_count: 1, member_limit: 20, effective_at: '', created_at: '' },
      { id: 6, account_id: 'acct6', company_id: 'c6', name: 'Paused', status: 'suspended', owner_user_id: 10, member_count: 0, member_limit: 20, effective_at: '', created_at: '' },
    ], total: 2 })
    const wrapper = mount(CompanyApplicationsView, mountOptions)
    await flushPromises()

    expect(memberAPI.listOrganizations).toHaveBeenCalled()
    const buttons = wrapper.findAll('[data-testid="manage-organization"]')
    expect(buttons).toHaveLength(1)
    await buttons[0].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/organizations/5/finance')
  })

  it('runs the console against the admin API with a visible tab bar and no personal actions', async () => {
    await router.replace('/admin/organizations/5/finance')
    const wrapper = mount(OrganizationConsoleView, { ...mountOptions, props: { organizationId: 5 } })
    await flushPromises()

    expect(createAdminOrganizationConsoleAPI).toHaveBeenCalledWith(5)
    expect(adminConsoleAPI.getContext).toHaveBeenCalled()
    expect(memberAPI.getContext).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="admin-console-banner"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="organization-tabs"]').attributes('hidden')).toBeUndefined()
    expect(wrapper.text()).not.toContain('organization.nameChange.action')

    await wrapper.get('#organization-tab-subscriptions').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/organizations/5/subscriptions')
    expect(adminConsoleAPI.listSubscriptions).toHaveBeenCalled()
    expect(plaza.listPlans).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('organization.subscriptions.createTitle')
  })
})
