import { describe, expect, it } from 'vitest'
import type { ModelIntro } from '@/api/admin/modelIntros'
import { modelIntroToUpsert, parseModelIntroImport, serializeModelIntros } from '../modelIntroTransfer'

const intro: ModelIntro = {
  model_key: 'openai/gpt-image-2', title: 'Image', description: '中文', description_en: 'English',
  cover_url: '/cover.png', sort_order: 7, scheduling_enabled: false,
  default_params: { prompt: { value: '', required: true, extra: { custom: true } }, legacy: ['a', 'b'] },
  output_fields: [{ key: 'images', type: 'array', description: 'Images', items: { properties: { url: { value: '' } } } }],
  result_field: 'images[*].url', result_type: 'image', created_at: 'old', updated_at: 'old'
}

describe('model intro transfer', () => {
  it('round trips all writable fields with nested schemas and disabled scheduling', () => {
    expect(parseModelIntroImport(serializeModelIntros([intro]))).toEqual([modelIntroToUpsert(intro)])
    expect(JSON.parse(serializeModelIntros([intro])).items[0]).not.toHaveProperty('created_at')
  })
  it('uses the same format for one item, a list and an empty export', () => {
    const second = { ...intro, model_key: 'another/model' }
    for (const rows of [[], [intro], [intro, second]]) {
      const exported = serializeModelIntros(rows)
      expect(Object.keys(JSON.parse(exported))).toEqual(['version', 'exported_at', 'items'])
      expect(parseModelIntroImport(exported)).toEqual(rows.map(modelIntroToUpsert))
    }
  })
  it('copies schemas independently of the source', () => {
    const copy = modelIntroToUpsert(intro)
    copy.default_params.legacy = []
    copy.output_fields[0].key = 'changed'
    expect(intro.default_params.legacy).toEqual(['a', 'b'])
    expect(intro.output_fields[0].key).toBe('images')
  })
  it('accepts raw records and defaults optional fields for older exports', () => {
    expect(parseModelIntroImport(JSON.stringify(intro))).toEqual([modelIntroToUpsert(intro)])
    expect(parseModelIntroImport('{"model_key":" model "}')[0]).toMatchObject({
      model_key: 'model', default_params: {}, output_fields: [], scheduling_enabled: true
    })
  })
  it.each([
    null, [], {}, { model_key: 1 }, { model_key: ' ' }, { items: {} }, { items: [intro, intro] },
    { version: 2, items: [intro] }, { ...intro, default_params: [] }, { ...intro, output_fields: [null] },
    { ...intro, output_fields: [{ key: 'foo', type: 'invalid' }] },
    { ...intro, scheduling_enabled: 'false' }, { ...intro, title: {} },
    { ...intro, result_type: 'audio' }, { ...intro, sort_order: 1.2 }
  ])('rejects malformed or duplicate items: %j', (value) => {
    expect(() => parseModelIntroImport(JSON.stringify(value))).toThrow()
  })
})
