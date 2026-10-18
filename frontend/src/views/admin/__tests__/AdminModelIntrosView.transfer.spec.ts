import { defineComponent, ref } from 'vue'
import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AdminModelIntrosView from '../AdminModelIntrosView.vue'
import type { ModelIntro } from '@/api/admin/modelIntros'

const api = vi.hoisted(() => ({ list: vi.fn(), listCandidates: vi.fn(), create: vi.fn(), update: vi.fn() }))
const showError = vi.hoisted(() => vi.fn())
vi.mock('@/api/admin', () => ({ adminAPI: { modelIntros: api } }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: vi.fn().mockResolvedValue({ items: [] }) } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: ref('zh') })
}))
vi.mock('@/composables/useDescriptionTranslation', () => ({
  provideDescriptionTranslation: () => ({ ready: ref(false) }), fetchModelsForKey: vi.fn()
}))
vi.mock('@/composables/useModelIntroDocExtract', () => ({ extractModelIntroFromDoc: vi.fn() }))

const source: ModelIntro = {
  model_key: 'openai/source', title: 'Source', description: '中文', description_en: 'English', cover_url: '/cover.png',
  default_params: { legacy: ['a', 'b'], prompt: { value: '', custom: 'preserve' } },
  output_fields: [{ key: 'url', label: 'Legacy label', type: 'string', description: 'URL' }],
  sort_order: 7, scheduling_enabled: false, result_field: 'url', result_type: 'image',
  created_at: '', updated_at: ''
}
const expected = () => {
  const { created_at: _created, updated_at: _updated, ...payload } = source
  return payload
}
const Dialog = defineComponent({
  props: ['show'], template: '<section v-if="show" data-dialog><slot /><slot name="footer" /></section>'
})
const wrappers: ReturnType<typeof shallowMount>[] = []
async function setup() {
  const wrapper = shallowMount(AdminModelIntrosView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters"/><slot name="table"/></div>' },
      DataTable: { props: ['data'], template: '<div><div v-for="row in data"><slot name="cell-actions" :row="row" /></div></div>' },
      BaseDialog: Dialog
    } }
  })
  wrappers.push(wrapper)
  await flushPromises()
  return wrapper
}
function button(wrapper: ReturnType<typeof shallowMount>, key: string, index = 0) {
  return wrapper.findAll('button').filter((b) => b.text() === key)[index]
}
async function upload(wrapper: ReturnType<typeof shallowMount>, value: unknown) {
  const input = wrapper.find('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [{ text: async () => JSON.stringify(value) }] })
  await input.trigger('change')
  await flushPromises()
}
async function save(wrapper: ReturnType<typeof shallowMount>) {
  await button(wrapper, 'common.save').trigger('click')
  await flushPromises()
}
beforeEach(() => {
  vi.clearAllMocks()
  api.list.mockResolvedValue({ items: [source], total: 1 })
  api.listCandidates.mockResolvedValue({ items: [] })
  api.create.mockResolvedValue({})
  api.update.mockResolvedValue({})
})
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('model intro transfer actions', () => {
  it('clones under a new key and preserves the entire configuration without mutating the source', async () => {
    const wrapper = await setup()
    await button(wrapper, 'admin.modelIntros.clone').trigger('click')
    const key = wrapper.findComponent({ name: 'Select' })
    expect(key.props('modelValue')).toBe('')
    key.vm.$emit('update:model-value', 'openai/cloned')
    await save(wrapper)
    expect(api.create).toHaveBeenCalledWith({ ...expected(), model_key: 'openai/cloned' })
    expect(api.update).not.toHaveBeenCalled()
    expect(source.model_key).toBe('openai/source')
  })
  it('does not allow a clone to use the original key', async () => {
    const wrapper = await setup()
    await button(wrapper, 'admin.modelIntros.clone').trigger('click')
    wrapper.findComponent({ name: 'Select' }).vm.$emit('update:model-value', source.model_key)
    await save(wrapper)
    expect(api.create).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.modelIntros.cloneKeyMustDiffer')
  })
  it('offers one import entry that accepts a single exported item', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await setup()
    expect(wrapper.findAll('button').filter(b => b.text() === 'admin.modelIntros.listImport')).toHaveLength(1)
    expect(wrapper.text()).not.toContain('admin.modelIntros.itemImport')
    expect(wrapper.findAll('input[type="file"]')).toHaveLength(1)
    await upload(wrapper, { version: 1, items: [source] })
    expect(api.update).toHaveBeenCalledWith(source.model_key, expected())
    expect(api.create).not.toHaveBeenCalled()
  })
  it('imports a list through the same entry, creating new keys and updating existing ones', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await setup()
    await upload(wrapper, { version: 1, items: [source, { ...source, model_key: 'new/model' }] })
    expect(api.create).toHaveBeenCalledWith({ ...expected(), model_key: 'new/model' })
    expect(api.update).toHaveBeenCalledWith(source.model_key, expected())
  })
  it('cancels an import without writing records', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const wrapper = await setup()
    await upload(wrapper, { version: 1, items: [source] })
    expect(api.create).not.toHaveBeenCalled()
    expect(api.update).not.toHaveBeenCalled()
  })
  it('exports only the selected item in a portable file with a safe filename', async () => {
    let content = ''
    vi.stubGlobal('Blob', class {
      constructor(parts: string[]) { content = parts.join('') }
    })
    const createObjectURL = vi.fn(() => 'blob:export')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    let filename = ''
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      filename = this.download
    })
    const wrapper = await setup()
    await button(wrapper, 'admin.modelIntros.itemExport').trigger('click')
    expect(JSON.parse(content)).toMatchObject({ version: 1, items: [expected()] })
    expect(filename).toBe('model-intro-openai_source.json')
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:export')
  })
  it('rejects duplicate keys without writing records or opening the editor', async () => {
    const wrapper = await setup()
    await upload(wrapper, { items: [source, source] })
    expect(showError).toHaveBeenCalledWith('admin.modelIntros.listImportFailed')
    expect(wrapper.find('[data-dialog]').exists()).toBe(false)
    expect(api.create).not.toHaveBeenCalled()
    expect(api.update).not.toHaveBeenCalled()
  })
})
