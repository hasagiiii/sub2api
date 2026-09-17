import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

import PlanEditDialog from '../PlanEditDialog.vue'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminGroup } from '@/types'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'payment.admin.subscriptionCnyPayPreview') return `preview ${params?.amount}`
      if (key === 'payment.admin.subscriptionCnyPayPreviewWithFee') return `fee ${params?.feeRate} ${params?.total}`
      return key
    },
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
  },
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: Boolean,
    title: String,
    width: String,
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: [String, Number],
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: String,
  },
  emits: ['update:modelValue'],
  setup(_props, { emit }) {
    const onChange = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      emit('update:modelValue', value === '' ? null : Number(value))
    }
    return { onChange }
  },
  template: `
    <select
      :value="modelValue ?? ''"
      @change="onChange"
    >
      <option value="">{{ placeholder }}</option>
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :data-platform="option.platform"
      >
        {{ option.label }}
      </option>
    </select>
  `,
})

const groupFixture = (overrides: Partial<AdminGroup>): AdminGroup => ({
  id: 1,
  name: 'OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'subscription',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: false,
  sort_order: 0,
  ...overrides,
})

function mountDialog({
  groups = [],
  paymentConfig = null,
}: {
  groups?: AdminGroup[]
  paymentConfig?: Record<string, unknown> | null
} = {}) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan: null,
      groups,
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog', () => {
  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 7.15,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 0,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })

  it('allows composite subscription groups for payment plans', () => {
    const wrapper = mountDialog({
      groups: [
        groupFixture({
          id: 10,
          name: 'OpenAI + Claude + Gemini + Grok',
          platform: 'composite',
          rate_multiplier: 1.2,
          subscription_type: 'subscription',
        }),
        groupFixture({
          id: 11,
          name: 'Standard OpenAI',
          platform: 'openai',
          subscription_type: 'standard',
        }),
      ],
    })

    // 分组是多选（复选框列表），不再是单选下拉。
    const labels = wrapper.findAll('input[type="checkbox"]').map(box => box.element.parentElement?.textContent?.trim())

    expect(labels).toContain('OpenAI + Claude + Gemini + Grok — composite (1.2x)')
    expect(labels).not.toContain('Standard OpenAI — openai (1x)')
  })

  // 打包授予：勾选多个分组后 payload 必须带上完整的 group_ids，并把首个选中的
  // 分组作为主分组写进 group_id（后端据此决定结账页/广场卡片的展示分组）。
  it('submits every selected group and keeps the first as the primary group', async () => {
    const createPlan = vi.mocked(adminPaymentAPI.createPlan)
    createPlan.mockClear()
    createPlan.mockResolvedValue({ data: {} } as never)

    const wrapper = mountDialog({
      groups: [
        groupFixture({ id: 10, name: 'Alpha', platform: 'openai', subscription_type: 'subscription' }),
        groupFixture({ id: 11, name: 'Beta', platform: 'gemini', subscription_type: 'subscription' }),
      ],
    })

    const boxes = wrapper.findAll('input[type="checkbox"]')
    await boxes[1].setValue(true)
    await boxes[0].setValue(true)
    await wrapper.find('input[type="number"]').setValue('9.99')
    await wrapper.find('form').trigger('submit')

    expect(createPlan).toHaveBeenCalledTimes(1)
    const payload = createPlan.mock.calls[0][0] as { group_ids: number[]; group_id: number }
    // 顺序即勾选顺序，首个是主分组。
    expect(payload.group_ids).toEqual([11, 10])
    expect(payload.group_id).toBe(11)
  })

  it('refuses to save when no group is selected', async () => {
    const createPlan = vi.mocked(adminPaymentAPI.createPlan)
    createPlan.mockClear()

    const wrapper = mountDialog({
      groups: [groupFixture({ id: 10, name: 'Alpha', platform: 'openai', subscription_type: 'subscription' })],
    })

    await wrapper.find('input[type="number"]').setValue('9.99')
    await wrapper.find('form').trigger('submit')

    expect(createPlan).not.toHaveBeenCalled()
  })
})
