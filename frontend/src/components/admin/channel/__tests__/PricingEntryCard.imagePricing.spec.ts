import { enableAutoUnmount, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import IntervalRow from '../IntervalRow.vue'
import {
  apiIntervalsToForm,
  createDefaultTimePricingForm,
  formIntervalsToAPI,
  validateIntervals,
  type PricingFormEntry,
} from '../types'

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)

function createEntry(intervals: PricingFormEntry['intervals'] = []): PricingFormEntry {
  return {
    models: [], billing_mode: 'image', intervals,
    input_price: null, output_price: null,
    cache_write_price: null, cache_read_price: null,
    image_input_price: null, image_output_price: null, per_request_price: null,
    time_pricing: createDefaultTimePricingForm(),
  }
}

function mountCard(entry = createEntry()) {
  return mount(PricingEntryCard, {
    props: { entry },
    global: { stubs: { ModelTagInput: true, Select: true, Icon: true } },
  })
}

function button(wrapper: VueWrapper, key: string) {
  return wrapper.findAll('button').find(item => item.text().includes('admin.channels.form.' + key))!
}

function input(row: VueWrapper, key: string) {
  return row.findAll<HTMLInputElement>('input').find(item =>
    item.element.parentElement?.querySelector('label')?.textContent?.includes('admin.channels.form.' + key),
  )!
}

async function acceptUpdate(wrapper: ReturnType<typeof mountCard>) {
  const entry = wrapper.emitted('update')!.at(-1)![0] as PricingFormEntry
  await wrapper.setProps({ entry })
  return entry
}

describe('PricingEntryCard image tiers', () => {
  it('loads, edits and serializes every existing standard tier field', async () => {
    const saved = {
      min_tokens: 128000, max_tokens: 256000,
      tier_label: '1K', resolution: '1080x1080', quality: 'low',
      per_request_price: 0.04, sort_order: 0,
    }
    const wrapper = mountCard(createEntry(apiIntervalsToForm([saved])))
    const row = wrapper.getComponent(IntervalRow)
    expect(row.findAll('input')).toHaveLength(6)
    expect(input(row, 'tierLabel').element.value).toBe('1K')
    expect(input(row, 'resolutionThreshold').element.value).toBe('1080x1080')
    expect(input(row, 'quality').element.value).toBe('low')
    expect(input(row, 'minTokens').element.value).toBe('128000')
    expect(input(row, 'maxTokens').element.value).toBe('256000')
    expect(formIntervalsToAPI(wrapper.props('entry').intervals)[0]).toMatchObject(saved)

    await input(row, 'tierLabel').setValue('')
    const cleared = await acceptUpdate(wrapper)
    expect(cleared.intervals[0].tier_label).toBe('')
    expect(validateIntervals(cleared.intervals, 'image', key => key)).toContain('imageTierValidation')

    for (const [field, value] of [
      ['tierLabel', '2k'], ['resolutionThreshold', '2160x2160'], ['quality', 'high'],
      ['minTokens', '2.72e5'], ['maxTokens', '1e6'], ['perRequestPrice', '0.08'],
    ]) {
      await input(row, field).setValue(value)
      await acceptUpdate(wrapper)
    }
    const intervals = wrapper.props('entry').intervals
    expect(validateIntervals(intervals, 'image', key => key)).toBeNull()
    expect(formIntervalsToAPI(intervals)[0]).toMatchObject({
      tier_label: '2K', resolution: '2160x2160', quality: 'high',
      min_tokens: 272000, max_tokens: 1000000, per_request_price: 0.08, max_pixels: null,
    })
    await input(row, 'maxTokens').setValue('')
    const updated = await acceptUpdate(wrapper)
    expect(formIntervalsToAPI(updated.intervals)[0].max_tokens).toBeNull()
  })

  it('restores resolution templates and allows extra quality variants after all three tiers exist', async () => {
    const wrapper = mountCard()
    await button(wrapper, 'addTier').trigger('click')
    const seeded = await acceptUpdate(wrapper)
    expect(seeded.intervals.map(iv => [iv.tier_label, iv.resolution, iv.quality])).toEqual([
      ['1K', '1024x1024', 'low'],
      ['2K', '2048x2048', 'low'],
      ['4K', '4096x4096', 'low'],
    ])
    expect(button(wrapper, 'addTier').attributes('disabled')).toBeUndefined()
    await button(wrapper, 'addTier').trigger('click')
    const added = await acceptUpdate(wrapper)
    expect(added.intervals).toHaveLength(4)
    expect(added.intervals.slice(0, 3)).toEqual(seeded.intervals)

    const rows = wrapper.findAllComponents(IntervalRow)
    await input(rows[0], 'perRequestPrice').setValue('0.04')
    await acceptUpdate(wrapper)
    for (const [field, value] of [
      ['tierLabel', '1K'], ['resolutionThreshold', '1024x1024'],
      ['quality', 'high'], ['perRequestPrice', '0.08'],
    ]) {
      await input(rows[3], field).setValue(value)
      await acceptUpdate(wrapper)
    }
    const intervals = wrapper.props('entry').intervals
    expect(validateIntervals(intervals, 'image', key => key)).toBeNull()
    const saved = formIntervalsToAPI(intervals)
    expect(saved).toHaveLength(2)
    expect(saved.map(iv => [iv.tier_label, iv.quality, iv.per_request_price])).toEqual([
      ['1K', 'low', 0.04], ['1K', 'high', 0.08],
    ])
  })

  it('keeps pixel tiers limited to maximum pixels and price, including an unlimited final tier', async () => {
    const wrapper = mountCard(createEntry(apiIntervalsToForm([{
      min_tokens: 0, max_tokens: null, tier_label: 'P1',
      max_pixels: 1000000, per_request_price: 0.03, sort_order: 0,
    }])))
    const row = wrapper.getComponent(IntervalRow)
    expect(row.findAll('input')).toHaveLength(2)
    expect(input(row, 'maxPixels').element.value).toBe('1000000')
    for (const key of ['tierLabel', 'resolutionThreshold', 'quality', 'minTokens', 'maxTokens']) {
      expect(input(row, key)).toBeUndefined()
    }
    expect(button(wrapper, 'addTier').attributes('disabled')).toBeDefined()
    await button(wrapper, 'addTier').trigger('click')
    expect(wrapper.emitted('update')).toBeUndefined()

    await input(row, 'maxPixels').setValue('2000000')
    await acceptUpdate(wrapper)
    await button(wrapper, 'addPixelTier').trigger('click')
    await acceptUpdate(wrapper)
    const lastRow = wrapper.findAllComponents(IntervalRow)[1]
    expect(lastRow.findAll('input')).toHaveLength(2)
    await input(lastRow, 'perRequestPrice').setValue('0.06')
    const updated = await acceptUpdate(wrapper)
    expect(validateIntervals(updated.intervals, 'image', key => key)).toBeNull()
    expect(formIntervalsToAPI(updated.intervals).map(iv => [iv.max_pixels, iv.per_request_price]))
      .toEqual([[2000000, 0.03], [null, 0.06]])
  })

  it('allows switching to pixel tiers only after removing the standard tiers', async () => {
    const wrapper = mountCard()
    await button(wrapper, 'addTier').trigger('click')
    await acceptUpdate(wrapper)
    expect(button(wrapper, 'addPixelTier').attributes('disabled')).toBeDefined()
    for (let i = 0; i < 3; i++) {
      await wrapper.getComponent(IntervalRow).get('button').trigger('click')
      await acceptUpdate(wrapper)
    }
    expect(wrapper.props('entry').intervals).toEqual([])
    expect(button(wrapper, 'addPixelTier').attributes('disabled')).toBeUndefined()
    await button(wrapper, 'addPixelTier').trigger('click')
    const updated = await acceptUpdate(wrapper)
    expect(updated.intervals[0]).toMatchObject({ image_tier_type: 'pixel', tier_label: 'P1' })
    expect(wrapper.getComponent(IntervalRow).findAll('input')).toHaveLength(2)
  })
})
