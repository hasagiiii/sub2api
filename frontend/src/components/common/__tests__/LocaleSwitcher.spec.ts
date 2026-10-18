import { DOMWrapper, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import LocaleSwitcher from '../LocaleSwitcher.vue'

const { setLocale } = vi.hoisted(() => ({ setLocale: vi.fn() }))

vi.mock('@/i18n', () => ({
  setLocale,
  availableLocales: [
    { code: 'en', name: 'English', flag: '🇺🇸' },
    { code: 'zh', name: '中文', flag: '🇨🇳' }
  ]
}))

vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'zh' } }) }))

const reload = vi.fn()

const mountSwitcher = () =>
  mount(LocaleSwitcher, { attachTo: document.body, global: { stubs: { Icon: true, transition: true } } })

const choose = async (wrapper: ReturnType<typeof mountSwitcher>, name: string) => {
  await wrapper.get('button').trigger('click')
  const option = new DOMWrapper(document.body).findAll('[role=menuitem]').find((button) => button.text().includes(name))
  await option!.trigger('click')
  await flushPromises()
}

describe('LocaleSwitcher', () => {
  beforeEach(() => {
    setLocale.mockReset().mockResolvedValue(undefined)
    reload.mockReset()
    vi.stubGlobal('location', { ...window.location, reload })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('opens the menu on hover', async () => {
    const wrapper = mountSwitcher()
    await wrapper.get('[data-test="locale-switcher"]').trigger('mouseenter')
    expect(document.body.textContent).toContain('English')
    expect(document.body.textContent).toContain('中文')
    wrapper.unmount()
  })

  // 分组名称等内容由后端按请求语言返回，已加载的数据要靠整页刷新换成新语言
  it('reloads the page after switching to another language', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, 'English')

    expect(setLocale).toHaveBeenCalledWith('en')
    expect(reload).toHaveBeenCalledTimes(1)
    expect(document.querySelector('[role=menu]')).toBeNull()
    expect(setLocale.mock.invocationCallOrder[0]).toBeLessThan(reload.mock.invocationCallOrder[0])
    wrapper.unmount()
  })

  it('does nothing when the current language is chosen again', async () => {
    const wrapper = mountSwitcher()

    await choose(wrapper, '中文')

    expect(setLocale).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
