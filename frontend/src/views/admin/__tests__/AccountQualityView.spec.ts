import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import AccountQualityView from '../AccountQualityView.vue'
import * as accountsAPI from '@/api/admin/accounts'
import scheduledTests from '@/api/admin/scheduledTests'
import { createQualityTemplate, deleteQualityTemplate, listQualityPlans, listQualityOperations, listQualityTemplates, updateQualityTemplate, type QualityRuleTemplate } from '@/api/admin/accountQuality'
import { useAccountQualityStore } from '@/stores/accountQuality'
import { useAuthStore } from '@/stores/auth'
import type { ScheduledTestPlan } from '@/types'
vi.mock('@/components/admin/operations/SmartOpsNav.vue', () => ({ default: { template: '<nav />' } }))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  const auth = reactive({ user: { id: 1, role: 'admin' } })
  return { useAuthStore: () => auth }
})
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key, te: () => true }) }))
vi.mock('@/api/admin/accountQuality', () => ({ listQualityPlans: vi.fn(), runQualityPlan: vi.fn(), listQualityOperations: vi.fn().mockResolvedValue({items:[],next_cursor:0}),
  listQualityTemplates: vi.fn().mockResolvedValue([]), createQualityTemplate: vi.fn(), updateQualityTemplate: vi.fn(), deleteQualityTemplate: vi.fn() }))
vi.mock('@/api/admin/scheduledTests', () => ({ default: { create: vi.fn(), update: vi.fn(), delete: vi.fn(), listResults: vi.fn(), getResult: vi.fn() } }))
vi.mock('@/api/admin/accounts', () => ({ list: vi.fn().mockResolvedValue({ items: [{ id: 1, name: 'Test account', platform: 'openai', type: 'oauth' }], total: 1 }) }))
vi.mock('@/api/admin/groups', () => ({ getModelAllowlistCandidates: vi.fn().mockResolvedValue(["test-judge"]), getAllIncludingInactive: vi.fn().mockResolvedValue([{ id: 21, name: 'Quality pool', status:'active', platform: 'openai' }]) }))
vi.mock('@/components/account/ModelWhitelistSelector.vue', () => ({ default: { props: ['modelValue'], template: '<div data-testid="model-selector">{{ modelValue.join(",") }}</div>' } }))
const mountView = () => mount(AccountQualityView, { global: { plugins: [createPinia()], stubs: { Teleport: true, AppLayout: { template: '<main><slot /></main>' } } } })
describe('quality operations', () => {
  beforeEach(() => { vi.clearAllMocks(); useAuthStore().user!.id = 1; vi.mocked(scheduledTests.delete).mockReset().mockResolvedValue(undefined); vi.mocked(scheduledTests.update).mockReset(); vi.mocked(accountsAPI.list).mockReset().mockResolvedValue({ items: [{ id: 1, name: 'Test account', platform: 'openai', type: 'oauth' }], total: 1 } as any); vi.mocked(listQualityPlans).mockResolvedValue([]); vi.mocked(listQualityOperations).mockResolvedValue({items:[],next_cursor:0})
    vi.mocked(listQualityTemplates).mockReset().mockResolvedValue([])
    vi.mocked(createQualityTemplate).mockReset().mockResolvedValue({ template: groupRule(), created: 0 })
    vi.mocked(updateQualityTemplate).mockReset().mockResolvedValue({ template: groupRule(), updated: 0, failed: 0, created: 0 })
    vi.mocked(deleteQualityTemplate).mockReset().mockResolvedValue({ deleted: 0 }) })
  const groupRule = (): QualityRuleTemplate => ({
    id: 7, account_filter: { group: '21', statuses: ['active'] }, model_id: 'group-model', cron_expression: '*/30 * * * *', enabled: true, max_results: 100,
    pelican_config: { question_kind: 'state_probe', prompt: '', reasoning_effort: 'high', parallel_count: 1,
      quality: { expected_answer: '', action: 'disable_scheduling', remove_group_ids: [], auto_restore: true } },
    plan_ids: [1], last_synced_at: null, created_at: '', updated_at: '',
  })
  const rules = (): ScheduledTestPlan[] => [1, 2, 3].map(id => ({
    id, account_id: id, account_name: `Account ${id}`, model_id: `model-${id}`, cron_expression: '*/30 * * * *', enabled: true,
    max_results: 100, auto_recover: false, last_run_at: null, next_run_at: null, created_at: '', updated_at: '',
    pelican_config: { question_kind: 'state_probe', prompt: '', reasoning_effort: 'high', parallel_count: 1,
      quality: { expected_answer: '', action: 'remove_groups', remove_group_ids: [id], auto_restore: false } },
  }))

  it('confirms and deletes checked rules including hidden search selections', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.get('[data-testid="quality-bulk-delete"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-plan-id="1"] .rule-checkbox input').setValue(true)
    await wrapper.get('.rule-search input').setValue('Account 2')
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-delete"]').trigger('click')
    const targets = wrapper.get('[data-testid="quality-delete-targets"]')
    expect(targets.text()).toContain('Account 1')
    expect(targets.text()).toContain('Account 2')
    expect(targets.text()).not.toContain('Account 3')
    expect(scheduledTests.delete).not.toHaveBeenCalled()
    vi.mocked(listQualityPlans).mockResolvedValue([rules()[2]])
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[1], [2]])
    expect(wrapper.find('[data-testid="quality-delete-targets"]').exists()).toBe(false)
    expect((wrapper.vm as any).selectedRuleIds).toEqual([])
    expect(listQualityOperations).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('deletes rules selected in the bulk account picker without checking edit fields', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(accountsAPI.list).mockResolvedValue({ items: [{ id: 2, name: 'Account 2' }, { id: 3, name: 'Account 3' }], total: 2 } as any)
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="quality-editor-delete"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-select-page"]').trigger('click')
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-editor-delete"]').trigger('click')
    vi.mocked(listQualityPlans).mockResolvedValue([rules()[0]])
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[2], [3]])
    expect(scheduledTests.update).not.toHaveBeenCalled()
    expect(wrapper.find('#quality-rule-form').exists()).toBe(false)
    wrapper.unmount()
  })

  it('continues batch deletion after an error and retries only failed rules', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(scheduledTests.delete).mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('delete denied')).mockResolvedValueOnce(undefined)
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-delete"]').trigger('click')
    vi.mocked(listQualityPlans).mockResolvedValue([rules()[1]])
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[1], [2], [3]])
    expect(wrapper.get('[data-testid="quality-delete-error"]').text()).toContain('#2: delete denied')
    expect((wrapper.vm as any).selectedRuleIds).toEqual([2])
    expect(wrapper.get('[data-testid="quality-delete-targets"]').findAll('li')).toHaveLength(1)
    vi.mocked(listQualityPlans).mockResolvedValue([])
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[1], [2], [3], [2]])
    expect(wrapper.find('[data-testid="quality-delete-targets"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('can cancel deletion and still delete a single rule from its editor', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    const vm = wrapper.vm as any
    vm.edit(rules()[0]); await flushPromises()
    await wrapper.get('[data-testid="quality-editor-delete"]').trigger('click')
    await wrapper.get('[data-testid="quality-cancel-delete"]').trigger('click')
    expect(scheduledTests.delete).not.toHaveBeenCalled()
    expect(wrapper.find('#quality-rule-form').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quality-delete-targets"]').exists()).toBe(false)
    await wrapper.get('[data-testid="quality-editor-delete"]').trigger('click')
    vi.mocked(listQualityPlans).mockRejectedValueOnce(new Error('refresh failed'))
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[1]])
    expect(useAccountQualityStore().plans.map(plan => plan.id)).toEqual([2, 3])
    expect(wrapper.find('#quality-rule-form').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps the confirmed rule IDs fixed if selection changes', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-plan-id="1"] .rule-checkbox input').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-delete"]').trigger('click')
    const vm = wrapper.vm as any
    vm.selectedRuleIds = [2, 3]
    vi.mocked(listQualityPlans).mockResolvedValue(rules().slice(1))
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[1]])
    expect(vm.selectedRuleIds).toEqual([2, 3])
    wrapper.unmount()
  })

  it.each(['unmount', 'identity'])('prevents duplicate submissions and stops deletion after %s changes', async (change) => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    let finish!: () => void
    vi.mocked(scheduledTests.delete).mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve }))
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-delete"]').trigger('click')
    await wrapper.get('[data-testid="quality-confirm-delete"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="quality-confirm-delete"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="quality-cancel-delete"]').attributes('disabled')).toBeDefined()
    await (wrapper.vm as any).confirmDelete()
    expect(scheduledTests.delete).toHaveBeenCalledTimes(1)
    if (change === 'unmount') wrapper.unmount()
    else { useAuthStore().user!.id = 2; await flushPromises() }
    finish(); await flushPromises()
    expect(vi.mocked(scheduledTests.delete).mock.calls).toEqual([[1]])
    expect(listQualityOperations).toHaveBeenCalledTimes(1)
    if (change === 'identity') {
      expect(wrapper.find('[data-testid="quality-delete-targets"]').exists()).toBe(false)
      expect(wrapper.find('[data-testid="quality-delete-error"]').exists()).toBe(false)
      wrapper.unmount()
    }
  })

  it('selects search matches independently of the history filter and preserves hidden selections', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-plan-id="1"] .rule-checkbox input').setValue(true)
    await wrapper.get('[data-plan-id="2"] .rule-select').trigger('click')
    expect(useAccountQualityStore().selectedPlanId).toBe(2)
    await wrapper.get('.rule-search input').setValue('Account 2')
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('.rule-search input').setValue('')
    expect(wrapper.get('[data-testid="quality-select-rules"]').element).toHaveProperty('indeterminate', true)
    expect(wrapper.get('[data-plan-id="1"] .rule-checkbox input').element).toHaveProperty('checked', true)
    expect(wrapper.get('[data-plan-id="2"] .rule-checkbox input').element).toHaveProperty('checked', true)
    expect(wrapper.get('[data-plan-id="3"] .rule-checkbox input').element).toHaveProperty('checked', false)
    await wrapper.get('[data-testid="quality-clear-rules"]').trigger('click')
    expect(wrapper.get('[data-testid="quality-bulk-edit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('opens bulk editing without preselection and selects only matching accounts with rules across pages', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(accountsAPI.list).mockResolvedValue({ items: [{ id: 2, name: 'Account 2' }, { id: 4, name: 'No rule' }], total: 51 } as any)
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#quality-rule-form input[type="checkbox"][value="4"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-account-group"]').setValue('21')
    await wrapper.get('[data-testid="quality-account-type"]').setValue('apikey')
    await wrapper.get('#quality-account-search').setValue('matching')
    await wrapper.get('#quality-account-search').trigger('keydown', { key: 'Enter' }); await flushPromises()
    vi.mocked(accountsAPI.list).mockClear()
      .mockResolvedValueOnce({ items: [{ id: 2 }, { id: 4 }], total: 51 } as any)
      .mockResolvedValueOnce({ items: [{ id: 3 }, { id: 2 }], total: 51 } as any)
    await wrapper.get('[data-testid="quality-select-all"]').trigger('click'); await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(2, 50, expect.objectContaining({ group: '21', type: 'apikey', search: 'matching' }))
    expect((wrapper.vm as any).bulkRuleIds).toEqual([2, 3])
    await wrapper.get('[data-testid="quality-account-type"]').setValue('oauth'); await flushPromises()
    expect((wrapper.vm as any).bulkRuleIds).toEqual([2, 3])
    await wrapper.get('[data-testid="quality-bulk-field-model"]').setValue(true)
    await wrapper.get('input[placeholder="gpt-6-astra"]').setValue('new-model')
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    expect(vi.mocked(scheduledTests.update).mock.calls).toEqual([[2, { model_id: 'new-model' }], [3, { model_id: 'new-model' }]])
    expect(scheduledTests.create).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('removes unsupported selections when switching to probes and excludes them from select all', async () => {
    const accounts = [
      { id: 1, name: 'OAuth', platform: 'openai', type: 'oauth' },
      { id: 2, name: 'Key', platform: 'openai', type: 'apikey' },
      { id: 3, name: 'Claude', platform: 'anthropic', type: 'oauth' },
      { id: 4, name: 'Setup', platform: 'openai', type: 'setup-token' },
    ]
    vi.mocked(accountsAPI.list).mockResolvedValue({ items: accounts, total: 4 } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    await wrapper.get('[data-testid="quality-select-page"]').trigger('click')
    expect(vm.selectedAccounts).toEqual([1, 2, 3, 4])
    await wrapper.get('[data-testid="quality-question-kind"]').setValue('state_probe')
    expect(vm.selectedAccounts).toEqual([1, 4])
    expect(wrapper.get('#quality-rule-form input[type="checkbox"][value="2"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-select-all"]').trigger('click'); await flushPromises()
    expect(vm.selectedAccounts).toEqual([1, 4])
    vm.form.pelican_config.quality.action = 'disable_scheduling'
    await vm.save()
    expect(vi.mocked(scheduledTests.create).mock.calls.map(([request]) => request.account_id)).toEqual([1, 4])
    wrapper.unmount()
  })

  it('prunes deleted selections after a refresh', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    vi.mocked(listQualityPlans).mockResolvedValue([])
    await useAccountQualityStore().refreshRules(true); await flushPromises()
    expect(wrapper.get('[data-testid="quality-bulk-edit"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('applies only checked fields, continues after a failure and retries only failed rules', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(scheduledTests.update).mockResolvedValueOnce(rules()[0]).mockRejectedValueOnce(new Error('Temporary failure')).mockResolvedValue(rules()[2])
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="quality-account-group"]').exists()).toBe(true)
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-bulk-field-model"]').setValue(true)
    await wrapper.get('input[placeholder="gpt-6-astra"]').setValue('new-model')
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    expect(vi.mocked(scheduledTests.update).mock.calls).toEqual([[1, { model_id: 'new-model' }], [2, { model_id: 'new-model' }], [3, { model_id: 'new-model' }]])
    expect(wrapper.text()).toContain('Temporary failure')
    expect(wrapper.find('#quality-rule-form').exists()).toBe(true)
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    expect(vi.mocked(scheduledTests.update).mock.calls.map(([id]) => id)).toEqual([1, 2, 3, 2])
    expect(wrapper.find('#quality-rule-form').exists()).toBe(false)
    wrapper.unmount()
  })


  it('merges nested changes into refreshed per-rule settings', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="quality-bulk-field-restore"]').setValue(true)
    const vm = wrapper.vm as any
    vm.form.pelican_config.quality.auto_restore = true
    const refreshed = rules(); refreshed[1].pelican_config!.quality!.remove_group_ids = [99]
    vi.mocked(listQualityPlans).mockResolvedValue(refreshed)
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    const bodies = vi.mocked(scheduledTests.update).mock.calls.map(([, body]) => body)
    expect(bodies.map(body => body.pelican_config!.quality!.remove_group_ids)).toEqual([[1], [99], [3]])
    expect(bodies.every(body => body.pelican_config!.quality!.auto_restore)).toBe(true)
    expect(bodies.every(body => !('model_id' in body) && !('enabled' in body))).toBe(true)
    wrapper.unmount()
  })

  it('stops before any write if a selected rule disappeared or refreshing fails', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="quality-bulk-field-enabled"]').setValue(true)
    vi.mocked(listQualityPlans).mockRejectedValueOnce(new Error('Refresh failed'))
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('Refresh failed')
    expect(scheduledTests.update).not.toHaveBeenCalled()
    vi.mocked(listQualityPlans).mockResolvedValue(rules().slice(0, 2))
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('qualityOps.ruleUnavailable')
    expect(scheduledTests.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('blocks duplicate saves and stops a running batch on unmount', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    let resolve!: (value: ScheduledTestPlan) => void
    vi.mocked(scheduledTests.update).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = mountView(); await flushPromises()
    await wrapper.get('[data-testid="quality-select-rules"]').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    await wrapper.get('[data-testid="quality-bulk-field-enabled"]').setValue(true)
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    expect(scheduledTests.update).toHaveBeenCalledTimes(1)
    wrapper.unmount(); resolve(rules()[0]); await flushPromises()
    expect(scheduledTests.update).toHaveBeenCalledTimes(1)
  })
  it.each(['oauth', 'apikey'])('combines %s type, group and search across pages', async (type) => {
    vi.mocked(accountsAPI.list).mockResolvedValue({ items: [{ id: 2, name: 'Matching account' }], total: 51 } as any)
    const wrapper = mountView(); await flushPromises()
    const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    await wrapper.get('[data-testid="quality-account-type"]').setValue(type)
    await wrapper.get('[data-testid="quality-account-group"]').setValue('21')
    await wrapper.get('#quality-account-search').setValue('  matching  ')
    await wrapper.get('#quality-account-search').trigger('keydown', { key: 'Enter' }); await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(1, 50, expect.objectContaining({ type, group: '21', search: 'matching', lite: 'true' }))
    await wrapper.get('button[aria-label="qualityOps.nextAccountPage"]').trigger('click'); await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(2, 50, expect.objectContaining({ type, group: '21', search: 'matching' }))
    await wrapper.get('[data-testid="quality-account-type"]').setValue(''); await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(1, 50, expect.objectContaining({ type: undefined, group: '21', search: 'matching' }))
    wrapper.unmount()
  })
  it('selects only eligible accounts on this page and keeps selections across filters', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue([{ id: 4, account_id: 1 }] as any)
    vi.mocked(accountsAPI.list).mockResolvedValue({ items: [{ id: 1, name: 'Rule exists' }, { id: 2, name: 'New account' }], total: 51 } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises(); vm.selectedAccounts = [99]
    await wrapper.get('[data-testid="quality-select-page"]').trigger('click')
    await wrapper.get('[data-testid="quality-select-page"]').trigger('click')
    expect(vm.selectedAccounts).toEqual([99, 2])
    expect(wrapper.get('input[type="checkbox"][value="1"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-account-group"]').setValue('ungrouped'); await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(1, 50, expect.objectContaining({ group: 'ungrouped' }))
    expect(vm.selectedAccounts).toEqual([99, 2])
    await wrapper.get('[data-testid="quality-clear-selection"]').trigger('click')
    expect(vm.selectedAccounts).toEqual([])
    wrapper.unmount()
  })
  it('selects all filtered pages without duplicate IDs or accounts with rules', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue([{ id: 4, account_id: 1 }] as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    await wrapper.get('[data-testid="quality-account-type"]').setValue('oauth')
    await wrapper.get('[data-testid="quality-account-group"]').setValue('21'); await flushPromises()
    await wrapper.get('#quality-account-search').setValue('demo')
    await wrapper.get('#quality-account-search').trigger('keydown', { key: 'Enter' }); await flushPromises()
    vm.selectedAccounts = [99, 2]
    vi.mocked(accountsAPI.list).mockClear()
      .mockResolvedValueOnce({ items: [{ id: 1 }, { id: 2 }], total: 51 } as any)
      .mockResolvedValueOnce({ items: [{ id: 2 }, { id: 3 }], total: 51 } as any)
    await wrapper.get('[data-testid="quality-select-all"]').trigger('click'); await flushPromises()
    expect(accountsAPI.list).toHaveBeenNthCalledWith(1, 1, 50, expect.objectContaining({ type: 'oauth', group: '21', search: 'demo', sort_by: 'id', sort_order: 'asc' }))
    expect(accountsAPI.list).toHaveBeenNthCalledWith(2, 2, 50, expect.objectContaining({ type: 'oauth', group: '21', search: 'demo' }))
    expect(vm.selectedAccounts).toEqual([99, 2, 3])
    vm.form.pelican_config.quality.judge = { group_id: 21, model_id: 'test-judge', prompt: 'grade' }
    vm.form.pelican_config.quality.action = 'disable_scheduling'
    await vm.save()
    expect(vi.mocked(scheduledTests.create).mock.calls.map(([request]) => request.account_id)).toEqual([99, 2, 3])
    wrapper.unmount()
  })
  it('keeps the original selection if a later bulk page fails and allows retry', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises(); vm.selectedAccounts = [99]
    vi.mocked(accountsAPI.list).mockResolvedValueOnce({ items: [{ id: 2 }], total: 51 } as any).mockRejectedValueOnce(new Error('Page failed'))
    await wrapper.get('[data-testid="quality-select-all"]').trigger('click'); await flushPromises()
    expect(vm.selectedAccounts).toEqual([99]); expect(wrapper.text()).toContain('Page failed')
    await wrapper.get('[data-testid="quality-select-all"]').trigger('click'); await flushPromises()
    expect(vm.selectedAccounts).toEqual([99, 1]); expect(wrapper.text()).not.toContain('Page failed')
    wrapper.unmount()
  })
  it.each(['filter', 'clear', 'close'])('discards an in-flight bulk selection after %s', async (action) => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    let resolve!: (value: any) => void
    vi.mocked(accountsAPI.list).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await wrapper.get('[data-testid="quality-select-all"]').trigger('click')
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeDefined()
    if (action === 'filter') await wrapper.get('[data-testid="quality-account-type"]').setValue('apikey')
    else if (action === 'clear') await wrapper.get('[data-testid="quality-clear-selection"]').trigger('click')
    else { vm.closeForm(); vm.newPlan(); vm.accountScope = 'manual' }
    resolve({ items: [{ id: 77 }], total: 51 }); await flushPromises()
    expect(vm.selectedAccounts).toEqual([])
    expect(vm.selectingAccounts).toBe(false)
    expect(accountsAPI.list).not.toHaveBeenCalledWith(2, 50, expect.anything())
    wrapper.unmount()
  })
  it('ignores old list responses after filters change and clears failed results', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    let resolve!: (value: any) => void
    vi.mocked(accountsAPI.list).mockImplementationOnce(() => new Promise(done => { resolve = done }))
    await wrapper.get('[data-testid="quality-account-type"]').setValue('oauth')
    vi.mocked(accountsAPI.list).mockResolvedValueOnce({ items: [{ id: 2, name: 'API result' }], total: 1 } as any)
    await wrapper.get('[data-testid="quality-account-type"]').setValue('apikey'); await flushPromises()
    resolve({ items: [{ id: 77, name: 'Stale OAuth result' }], total: 1 }); await flushPromises()
    expect(vm.accounts.map((account: any) => account.id)).toEqual([2])
    vi.mocked(accountsAPI.list).mockRejectedValueOnce(new Error('Search failed'))
    await wrapper.get('[data-testid="quality-account-group"]').setValue('21'); await flushPromises()
    expect(vm.accounts).toEqual([])
    expect(wrapper.text()).toContain('Search failed')
    expect(wrapper.get('[data-testid="quality-select-all"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
  it('prefills the test model and submits it for a new rule', async () => {
    const wrapper = mountView(); await flushPromises()
    const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    expect(wrapper.find('input[placeholder="gpt-6-astra"]').element).toHaveProperty('value', 'gpt-6-astra')
    vm.selectedAccounts = [1]
    vm.form.pelican_config.quality.judge = { group_id: 21, model_id: 'test-judge', prompt: 'grade semantically' }
    vm.form.pelican_config.quality.remove_group_ids = [21]
    await vm.save()
    expect(scheduledTests.create).toHaveBeenCalledWith(expect.objectContaining({ account_id: 1, model_id: 'gpt-6-astra' }))
    wrapper.unmount()
  })
  it('requires explicit group selection and keeps automatic restoration opt-in', async () => {
    const wrapper = mountView(); await flushPromises()
    const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises(); vm.selectedAccounts = [1]; vm.form.pelican_config.quality.judge = {group_id:21,model_id:'test-judge',prompt:'grade semantically'}; vm.form.model_id = 'test-model'
    await vm.save()
    expect(scheduledTests.create).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toContain('qualityOps.selectGroups')
    vm.form.pelican_config.quality.remove_group_ids = [21]
    await vm.save()
    expect(scheduledTests.create).toHaveBeenCalledWith(expect.objectContaining({ account_id: 1, auto_recover: false, pelican_config: expect.objectContaining({ quality: { expected_answer: '21', action: 'remove_groups', remove_group_ids: [21], auto_restore: false, judge: {group_id:21,model_id:'test-judge',prompt:'grade semantically'} } }) }))
    wrapper.unmount()
  })
  it('retries only accounts that were not created before a partial batch failure', async () => {
    vi.mocked(scheduledTests.create).mockResolvedValueOnce({ id: 1 } as any).mockRejectedValueOnce(new Error('failed')).mockResolvedValueOnce({ id: 2 } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; vm.selectedAccounts = [1, 2]; vm.form.pelican_config.quality.judge = {group_id:21,model_id:'test-judge',prompt:'grade semantically'}; vm.form.model_id = 'test-model'; vm.form.pelican_config.quality.action = 'disable_scheduling'
    await vm.save(); expect(vm.selectedAccounts).toEqual([2]); expect(vm.showForm).toBe(true)
    await vm.save()
    expect(vi.mocked(scheduledTests.create).mock.calls.map(([request]) => request.account_id)).toEqual([1, 2, 2])
    wrapper.unmount()
  })
  it('shows one operation row per round and loads response bodies only on demand', async () => {
    const operation = { id: 10, plan_id: 4, account_id: 1, account_name: 'Test account', status: 'success', error_message: '', quality_action: 'restored', passed_count: 2, total_count: 2, result_ids: [10, 9], pelican_config: { quality: { action: 'remove_groups', remove_group_ids: [21] } } }
    vi.mocked(listQualityOperations).mockResolvedValueOnce({ items: [operation] as any, next_cursor: 10 })
    vi.mocked(scheduledTests.getResult).mockResolvedValue({ id: 10, status: 'success', error_message: '', response_text: '21个。', quality_judgment: { verdict: 'correct', reason: 'same answer', model_id: 'custom-judge' } } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    expect(wrapper.text()).toContain('qualityOps.operations')
    expect(wrapper.text()).toContain('2 / 2')
    expect(wrapper.text()).toContain('qualityOps.groupsRestored')
    expect(scheduledTests.getResult).not.toHaveBeenCalled()
    await vm.operationDetails(operation)
    expect(scheduledTests.getResult).toHaveBeenCalledWith(4, 10)
    expect(scheduledTests.getResult).not.toHaveBeenCalledWith(4, 9)
    await vm.selectResult(vm.results[1])
    expect(scheduledTests.getResult).toHaveBeenCalledWith(4, 9)
    expect(wrapper.text()).toContain('same answer')
    await vm.moreOperations(); expect(listQualityOperations).toHaveBeenLastCalledWith(10)
    wrapper.unmount()
  })
  it('requires the operator to select a judge model and group', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; vm.selectedAccounts = [1]; vm.form.model_id = 'test-model'
    await vm.save(); expect(scheduledTests.create).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toContain('qualityOps.configureJudge')
    wrapper.unmount()
  })
  it('saves a state probe rule without a question, reference answer, judge or parallel runs', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); vm.accountScope = 'manual'; await flushPromises()
    vm.selectedAccounts = [1]; vm.form.pelican_config.parallel_count = 3
    await wrapper.find('[data-testid="quality-question-kind"]').setValue('state_probe')
    expect(wrapper.find('[data-testid="quality-probe-hint"]').exists()).toBe(true)
    expect(wrapper.find('#quality-prompt').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('qualityOps.judgeTitle')
    vm.form.pelican_config.quality.remove_group_ids = [21]
    await vm.save()
    const request = vi.mocked(scheduledTests.create).mock.calls[0][0] as any
    expect(request).toMatchObject({ account_id: 1, model_id: 'gpt-6-astra', pelican_config: { question_kind: 'state_probe', prompt: '', parallel_count: 1, quality: { expected_answer: '', action: 'remove_groups', remove_group_ids: [21], auto_restore: false } } })
    expect(request.pelican_config.quality).not.toHaveProperty('judge')
    wrapper.unmount()
  })
  it('switching a probe rule back to candy restores the question and judge fields', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.edit({ id: 5, account_id: 1, model_id: 'gpt-6-astra', cron_expression: '*/30 * * * *', enabled: true, max_results: 100, pelican_config: { question_kind: 'state_probe', prompt: '', reasoning_effort: 'high', parallel_count: 1, quality: { expected_answer: '', action: 'disable_scheduling', remove_group_ids: [], auto_restore: true } } })
    await flushPromises()
    expect(wrapper.find('#quality-prompt').exists()).toBe(false)
    await wrapper.find('[data-testid="quality-question-kind"]').setValue('candy')
    expect(wrapper.find('#quality-prompt').exists()).toBe(true)
    expect(vm.form.pelican_config.quality.expected_answer).toBe('21')
    expect(vm.form.pelican_config.quality.judge).toEqual({ group_id: 0, model_id: '', prompt: 'qualityOps.defaultJudgePrompt' })
    await vm.save()
    expect(scheduledTests.update).not.toHaveBeenCalled()
    expect(wrapper.find('[role="alert"]').text()).toContain('qualityOps.configureJudge')
    wrapper.unmount()
  })
  it('tags probe rules and labels probe results as full capability, degraded or inconclusive', async () => {
    const probe = { question_kind: 'state_probe', prompt: '', reasoning_effort: 'high', parallel_count: 1, quality: { expected_answer: '', action: 'disable_scheduling', remove_group_ids: [], auto_restore: false } }
    vi.mocked(listQualityPlans).mockResolvedValue([{ id: 1, account_id: 1, account_name: 'Probe account', model_id: 'gpt-6-astra', cron_expression: '*/30 * * * *', enabled: true, max_results: 100, pelican_config: probe }] as any)
    vi.mocked(scheduledTests.listResults).mockResolvedValue([
      { id: 7, status: 'failed', error_message: 'state_degraded', pelican_config: probe },
      { id: 6, status: 'failed', error_message: 'state_probe_inconclusive: network', pelican_config: probe },
      { id: 5, status: 'success', error_message: '', pelican_config: probe }
    ] as any)
    vi.mocked(scheduledTests.getResult).mockResolvedValue({ id: 7, status: 'failed', error_message: 'state_degraded', response_text: '判定：降智', pelican_config: probe, quality_judgment: { verdict: 'incorrect', reason: 'new ticket' } } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    expect(wrapper.find('[data-testid="quality-probe-tag"]').exists()).toBe(true)
    expect(wrapper.find('.rule-warning').exists()).toBe(false)
    await vm.history(vm.plans[0]); await flushPromises()
    const labels = wrapper.findAll('.result-navigation button').map(button => button.text())
    expect(labels[0]).toContain('qualityOps.probeDegraded')
    expect(labels[1]).toContain('qualityOps.probeInconclusive')
    expect(labels[2]).toContain('qualityOps.probeHealthy')
    expect(wrapper.find('[data-testid="quality-result-badge"]').text()).toBe('qualityOps.probeDegraded')
    expect(wrapper.find('pre').text()).toContain('判定：降智')
    expect(wrapper.find('.answer-reference').exists()).toBe(false)
    expect(wrapper.find('.judge-reason').exists()).toBe(false)
    wrapper.unmount()
  })



  it('defaults new rules to normal accounts and saves the matching filter as a group rule', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(accountsAPI.list).mockImplementation(async (page: number) => (page === 1
      ? { items: [{ id: 1, name: 'Has rule', platform: 'openai', type: 'oauth' }, { id: 4, name: 'OAuth', platform: 'openai', type: 'oauth' }, { id: 5, name: 'Key', platform: 'openai', type: 'apikey' }], total: 51 }
      : { items: [{ id: 6, name: 'Setup', platform: 'openai', type: 'setup-token' }, { id: 4, name: 'OAuth', platform: 'openai', type: 'oauth' }], total: 51 }) as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); await flushPromises()
    expect(accountsAPI.list).toHaveBeenLastCalledWith(1, 50, expect.objectContaining({ status: 'active' }))
    expect((wrapper.get('[data-testid="quality-account-status-active"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="quality-account-scope-all"]').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.find('[data-testid="quality-select-all"]').exists()).toBe(false)
    expect(wrapper.get('#quality-rule-form input[type="checkbox"][value="4"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="quality-account-count"]').text()).toBe('qualityOps.accountsMatched')
    expect(vm.accountsTotal).toBe(51)
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="quality-account-scope-manual"]').setValue(true)
    expect(wrapper.find('[data-testid="quality-select-all"]').exists()).toBe(true)
    expect(wrapper.get('#quality-rule-form input[type="checkbox"][value="4"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="quality-account-count"]').text()).toBe('qualityOps.selected')
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="quality-account-scope-all"]').setValue(true)

    await wrapper.get('[data-testid="quality-question-kind"]').setValue('state_probe')
    vm.form.pelican_config.quality.action = 'disable_scheduling'; await flushPromises()
    vi.mocked(accountsAPI.list).mockClear()
    vi.mocked(createQualityTemplate).mockResolvedValueOnce({ template: groupRule(), created: 2 })
    await wrapper.get('#quality-rule-form').trigger('submit'); await flushPromises()
    // 由后端按筛选给现在和之后匹配的账户建规则，前端不再翻页逐个创建。
    expect(accountsAPI.list).not.toHaveBeenCalledWith(2, 50, expect.anything())
    expect(scheduledTests.create).not.toHaveBeenCalled()
    expect(createQualityTemplate).toHaveBeenCalledTimes(1)
    const body = vi.mocked(createQualityTemplate).mock.calls[0][0]
    expect(body.account_filter).toEqual({ group: undefined, type: undefined, search: undefined, statuses: ['active'] })
    expect(body.pelican_config).toMatchObject({ question_kind: 'state_probe', quality: { action: 'disable_scheduling' } })
    expect(body.cron_expression).toBeTruthy()
    expect(wrapper.get('.workspace-message[role="status"]').text()).toContain('qualityOps.templateCreated')
    expect(vm.showForm).toBe(false)
    wrapper.unmount()
  })
  it('filters by any checked account status and by every status when none is checked', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); await flushPromises()
    await wrapper.get('[data-testid="quality-account-status-rate_limited"]').setValue(true); await flushPromises()
    expect(vm.accountStatuses).toEqual(['active', 'rate_limited'])
    expect(accountsAPI.list).toHaveBeenLastCalledWith(1, 50, expect.objectContaining({ status: 'active,rate_limited' }))
    await wrapper.get('[data-testid="quality-account-status-active"]').setValue(false)
    await wrapper.get('[data-testid="quality-account-status-rate_limited"]').setValue(false); await flushPromises()
    expect(vi.mocked(accountsAPI.list).mock.lastCall![2]).toHaveProperty('status', undefined)
    vm.closeForm(); vm.newPlan(); await flushPromises()
    expect(vm.accountStatuses).toEqual(['active'])
    expect(vm.accountScope).toBe('all')
    wrapper.unmount()
  })
  it('saves a group rule even when no account matches yet so later accounts get it', async () => {
    vi.mocked(accountsAPI.list).mockResolvedValue({ items: [], total: 0 } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); await flushPromises()
    await wrapper.get('[data-testid="quality-account-group"]').setValue('21'); await flushPromises()
    expect(wrapper.get('button[form="quality-rule-form"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="quality-question-kind"]').setValue('state_probe')
    vm.form.pelican_config.quality.action = 'disable_scheduling'
    await vm.save(); await flushPromises()
    expect(createQualityTemplate).toHaveBeenCalledWith(expect.objectContaining({ account_filter: expect.objectContaining({ group: '21', statuses: ['active'] }) }))
    expect(scheduledTests.create).not.toHaveBeenCalled()
    expect(wrapper.get('.workspace-message[role="status"]').text()).toContain('qualityOps.templateCreatedNone')
    expect(vm.showForm).toBe(false)
    wrapper.unmount()
  })
  it('does not page through accounts when the form itself is incomplete', async () => {
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); await flushPromises()
    vi.mocked(accountsAPI.list).mockClear()
    await vm.save()
    expect(wrapper.find('[role="alert"]').text()).toContain('qualityOps.configureJudge')
    expect(accountsAPI.list).not.toHaveBeenCalled()
    expect(createQualityTemplate).not.toHaveBeenCalled()
    expect(vm.selectedAccounts).toEqual([])
    wrapper.unmount()
  })
  it('keeps the form open when saving a group rule fails and saves on retry', async () => {
    vi.mocked(createQualityTemplate).mockRejectedValueOnce(new Error('failed')).mockResolvedValueOnce({ template: groupRule(), created: 3 })
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.newPlan(); await flushPromises()
    await wrapper.get('[data-testid="quality-question-kind"]').setValue('state_probe')
    vm.form.pelican_config.quality.action = 'disable_scheduling'
    await vm.save()
    expect(wrapper.find('[role="alert"]').text()).toContain('failed')
    expect(vm.showForm).toBe(true)
    await vm.save()
    expect(createQualityTemplate).toHaveBeenCalledTimes(2)
    expect(scheduledTests.create).not.toHaveBeenCalled()
    expect(vm.showForm).toBe(false)
    wrapper.unmount()
  })
  it('lists group rules and tags the account rules they created', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(listQualityTemplates).mockResolvedValue([groupRule()])
    const wrapper = mountView(); await flushPromises()
    const card = wrapper.get('[data-template-id="7"]')
    expect(card.get('[data-testid="quality-template-filter"]').text()).toBe('Quality pool · qualityOps.allAccountTypes · admin.accounts.status.active')
    expect(card.text()).toContain('group-model')
    expect(wrapper.find('[data-plan-id="1"] [data-testid="quality-template-tag"]').exists()).toBe(true)
    expect(wrapper.find('[data-plan-id="2"] [data-testid="quality-template-tag"]').exists()).toBe(false)

    await card.get('[data-testid="quality-template-toggle"]').trigger('click'); await flushPromises()
    expect(updateQualityTemplate).toHaveBeenCalledWith(7, { enabled: false })
    expect(scheduledTests.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('shows group rules even before any account rule exists', async () => {
    vi.mocked(listQualityTemplates).mockResolvedValue([{ ...groupRule(), plan_ids: [] }])
    const wrapper = mountView(); await flushPromises()
    expect(wrapper.find('[data-template-id="7"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="rules-scroll"] .panel-empty').exists()).toBe(false)
    wrapper.unmount()
  })
  it('edits a group rule through its filter and pushes the change to every rule it created', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(listQualityTemplates).mockResolvedValue([groupRule()])
    vi.mocked(updateQualityTemplate).mockResolvedValueOnce({ template: groupRule(), updated: 1, failed: 0, created: 0 })
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    await wrapper.get('[data-template-id="7"] [data-testid="quality-template-edit"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="quality-template-edit-hint"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quality-account-scope-all"]').exists()).toBe(false)
    expect(vm.accountGroup).toBe('21')
    expect(vm.accountStatuses).toEqual(['active'])
    expect(vm.form.model_id).toBe('group-model')
    expect(vm.initialForm).toBe(vm.formSnapshot())
    vm.form.model_id = 'new-model'
    await vm.save(); await flushPromises()
    expect(updateQualityTemplate).toHaveBeenCalledWith(7, expect.objectContaining({ model_id: 'new-model', account_filter: expect.objectContaining({ group: '21', statuses: ['active'] }) }))
    expect(scheduledTests.create).not.toHaveBeenCalled()
    expect(scheduledTests.update).not.toHaveBeenCalled()
    expect(wrapper.get('.workspace-message[role="status"]').text()).toContain('qualityOps.templateUpdated')
    expect(vm.showForm).toBe(false)
    wrapper.unmount()
  })
  it('keeps a group rule edit open when some of its rules could not be updated', async () => {
    vi.mocked(listQualityTemplates).mockResolvedValue([groupRule()])
    vi.mocked(updateQualityTemplate).mockResolvedValueOnce({ template: groupRule(), updated: 1, failed: 2, created: 0 })
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.editTemplate(vm.templates[0]); await flushPromises()
    await vm.save(); await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('qualityOps.templateUpdatePartial')
    expect(vm.showForm).toBe(true)
    expect(vm.initialForm).toBe(vm.formSnapshot())
    wrapper.unmount()
  })
  it('deletes a group rule with or without the rules it created', async () => {
    for (const keep of [false, true]) {
      vi.mocked(listQualityPlans).mockResolvedValue(rules())
      vi.mocked(listQualityTemplates).mockResolvedValue([groupRule()])
      vi.mocked(deleteQualityTemplate).mockClear().mockResolvedValueOnce({ deleted: keep ? 0 : 1 })
      const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
      vm.editTemplate(vm.templates[0]); await flushPromises()
      await wrapper.get('[data-testid="quality-delete-template"]').trigger('click'); await flushPromises()
      const checkbox = wrapper.get('[data-testid="quality-delete-template-plans"]')
      expect((checkbox.element as HTMLInputElement).checked).toBe(true)
      if (keep) await checkbox.setValue(false)
      await wrapper.get('[data-testid="quality-confirm-delete-template"]').trigger('click'); await flushPromises()
      expect(deleteQualityTemplate).toHaveBeenCalledWith(7, !keep)
      expect(wrapper.get('.workspace-message[role="status"]').text()).toContain(keep ? 'qualityOps.templateDeletedKept' : 'qualityOps.templateDeleted')
      expect(vm.showForm).toBe(false)
      wrapper.unmount()
    }
  })
  it('explains that an account rule belongs to a group rule when editing it', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    vi.mocked(listQualityTemplates).mockResolvedValue([groupRule()])
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    vm.edit(vm.plans[0]); await flushPromises()
    expect(wrapper.find('[data-testid="quality-plan-template-hint"]').exists()).toBe(true)
    vm.showForm = false; await flushPromises()
    vm.edit(vm.plans[1]); await flushPromises()
    expect(wrapper.find('[data-testid="quality-plan-template-hint"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('hides the scope switch in bulk editing and lists rules on accounts in any status', async () => {
    vi.mocked(listQualityPlans).mockResolvedValue(rules())
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    await wrapper.get('[data-plan-id="1"] .rule-checkbox input').setValue(true)
    await wrapper.get('[data-testid="quality-bulk-edit"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="quality-account-scope-all"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="quality-select-all"]').exists()).toBe(true)
    expect(vm.accountStatuses).toEqual([])
    expect(vi.mocked(accountsAPI.list).mock.lastCall![2]).toHaveProperty('status', undefined)
    wrapper.unmount()
  })
  it('renders returned model content as text', async () => {
    vi.mocked(scheduledTests.listResults).mockResolvedValue([{ id: 4, status: 'failed', error_message: 'answer_mismatch', quality_action: 'groups_removed' }] as any)
    vi.mocked(scheduledTests.getResult).mockResolvedValue({ id: 4, status: 'failed', error_message: 'answer_mismatch', response_text: '<img src=x onerror=alert(1)>', quality_action: 'groups_removed' } as any)
    const wrapper = mountView(); await flushPromises(); const vm = wrapper.vm as any
    await vm.history({ id: 1 })
    expect(wrapper.find('pre').text()).toContain('<img')
    expect(wrapper.find('pre img').exists()).toBe(false)
    wrapper.unmount()
  })
})
