import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

import OpsSettingsDialog from '../OpsSettingsDialog.vue'

const { opsAPI, showError } = vi.hoisted(() => ({
  opsAPI: {
    getAlertRuntimeSettings: vi.fn(), getEmailNotificationConfig: vi.fn(),
    getAdvancedSettings: vi.fn(), getMetricThresholds: vi.fn(),
    updateAlertRuntimeSettings: vi.fn(), updateEmailNotificationConfig: vi.fn(),
    updateAdvancedSettings: vi.fn(), updateMetricThresholds: vi.fn(),
  },
  showError: vi.fn(),
}))
vi.mock('@/api/admin/ops', () => ({ opsAPI }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess: vi.fn() }) }))

type View = {
  gatewayDebugUserInput: string
  addGatewayDebugUser: () => void
  removeGatewayDebugUser: (email: string) => void
  saveAllSettings: () => Promise<void>
}

function advancedSettings(emails?: string[]) {
  return {
    data_retention: { error_log_retention_days: 7, minute_metrics_retention_days: 7, hourly_metrics_retention_days: 7 },
    openai_account_quota_auto_pause: { default_threshold_5h: 0, default_threshold_7d: 0 },
    gateway_debug_log_enabled: true,
    gateway_debug_resp_enabled: false,
    ...(emails ? { gateway_debug_log_user_emails: emails } : {}),
  }
}

async function openDialog() {
  const wrapper = shallowMount(OpsSettingsDialog, {
    props: { show: false },
    global: { stubs: { BaseDialog: { template: '<div><slot name="footer" /></div>' } } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return { wrapper, vm: wrapper.vm as unknown as View }
}

function savedAdvanced() {
  expect(opsAPI.updateAdvancedSettings).toHaveBeenCalledTimes(1)
  return opsAPI.updateAdvancedSettings.mock.calls[0][0]
}

describe('ops settings gateway debug user whitelist', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    opsAPI.getAlertRuntimeSettings.mockResolvedValue({ evaluation_interval_seconds: 30 })
    opsAPI.getEmailNotificationConfig.mockResolvedValue({ alert: { enabled: false, recipients: [] }, report: { enabled: false, recipients: [] } })
    opsAPI.getMetricThresholds.mockResolvedValue({ sla_percent_min: 98 })
    opsAPI.getAdvancedSettings.mockResolvedValue(advancedSettings(['alice@example.com']))
  })

  it('adds normalized, de-duplicated emails and submits them with advanced settings', async () => {
    const { vm } = await openDialog()
    vm.gatewayDebugUserInput = ' Bob@Example.com, alice@example.com；carol@example.com '
    vm.addGatewayDebugUser()
    expect(vm.gatewayDebugUserInput).toBe('')

    vm.removeGatewayDebugUser('alice@example.com')
    await vm.saveAllSettings()
    await flushPromises()

    expect(savedAdvanced().gateway_debug_log_user_emails).toEqual(['bob@example.com', 'carol@example.com'])
    expect(showError).not.toHaveBeenCalled()
  })

  it('rejects invalid emails and keeps the input for correction', async () => {
    const { vm } = await openDialog()
    vm.gatewayDebugUserInput = 'not-an-email'
    vm.addGatewayDebugUser()
    expect(showError).toHaveBeenCalledWith('common.invalidEmail')
    expect(vm.gatewayDebugUserInput).toBe('not-an-email')

    await vm.saveAllSettings()
    expect(opsAPI.updateAdvancedSettings).not.toHaveBeenCalled()
  })

  it('includes a typed but not yet added email when saving', async () => {
    const { vm } = await openDialog()
    vm.gatewayDebugUserInput = 'dave@example.com'
    await vm.saveAllSettings()
    await flushPromises()
    expect(savedAdvanced().gateway_debug_log_user_emails).toEqual(['alice@example.com', 'dave@example.com'])
  })

  it('submits an empty list for older payloads without the field', async () => {
    opsAPI.getAdvancedSettings.mockResolvedValue(advancedSettings())
    const { vm } = await openDialog()
    await vm.saveAllSettings()
    await flushPromises()
    expect(savedAdvanced().gateway_debug_log_user_emails).toEqual([])
  })
})
