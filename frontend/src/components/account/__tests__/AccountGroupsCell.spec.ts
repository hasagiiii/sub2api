import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountGroupsCell from '../AccountGroupsCell.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const groups = [
  { id: 1, name: 'Anthropic', platform: 'anthropic', subscription_type: 'standard', rate_multiplier: 1 },
  { id: 2, name: 'OpenAI', platform: 'openai', subscription_type: 'standard', rate_multiplier: 1 },
  { id: 3, name: 'Gemini', platform: 'gemini', subscription_type: 'standard', rate_multiplier: 1 },
  { id: 4, name: 'Composite', platform: 'composite', subscription_type: 'standard', rate_multiplier: 1 },
  { id: 5, name: 'Grok', platform: 'grok', subscription_type: 'standard', rate_multiplier: 1 },
] as any

describe('AccountGroupsCell', () => {
  it('does not clip the visible group badges when they wrap to a second row', () => {
    const wrapper = mount(AccountGroupsCell, {
      props: { groups },
      global: {
        stubs: {
          GroupBadge: {
            props: ['name'],
            template: '<span class="group-badge">{{ name }}</span>'
          }
        }
      }
    })

    const list = wrapper.get('[data-test="account-groups-list"]')
    expect(list.classes()).not.toContain('overflow-hidden')
    expect(wrapper.findAll('.group-badge')).toHaveLength(3)
    expect(wrapper.text()).toContain('+2')
  })
})
