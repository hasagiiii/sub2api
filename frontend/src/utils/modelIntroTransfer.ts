import type { ModelIntro, UpsertModelIntroRequest } from '@/api/admin/modelIntros'

/** Copy writable fields only, without retaining references to the source schema. */
export function modelIntroToUpsert(row: ModelIntro | UpsertModelIntroRequest): UpsertModelIntroRequest {
  return JSON.parse(JSON.stringify({
    model_key: String(row.model_key || '').trim(),
    title: row.title || '',
    description: row.description || '',
    description_en: row.description_en || '',
    cover_url: row.cover_url || '',
    default_params: isObject(row.default_params) ? row.default_params : {},
    sort_order: Number.isFinite(row.sort_order) ? row.sort_order : 0,
    scheduling_enabled: row.scheduling_enabled !== false,
    output_fields: Array.isArray(row.output_fields) ? row.output_fields : [],
    result_field: row.result_field || '',
    result_type: row.result_type === 'image' ? 'image' : 'video'
  }))
}

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

/** Single and list exports share the same envelope; accept legacy raw records too. */
export function parseModelIntroImport(text: string): UpsertModelIntroRequest[] {
  const value: unknown = JSON.parse(text)
  if (!isObject(value)) throw new Error('invalid model intro')
  let rows: unknown[]
  if ('items' in value) {
    if (value.version !== undefined && value.version !== 1) throw new Error('unsupported version')
    if (!Array.isArray(value.items)) throw new Error('invalid items')
    rows = value.items
  } else {
    rows = [value]
  }
  const items = rows.map(parseModelIntro)
  if (new Set(items.map(item => item.model_key)).size !== items.length) throw new Error('duplicate model_key')
  return items
}

function parseModelIntro(value: unknown): UpsertModelIntroRequest {
  if (!isObject(value) || typeof value.model_key !== 'string' || !value.model_key.trim()) {
    throw new Error('missing model_key')
  }
  for (const key of ['title', 'description', 'description_en', 'cover_url', 'result_field']) {
    if (value[key] !== undefined && typeof value[key] !== 'string') throw new Error(`invalid ${key}`)
  }
  if (value.default_params !== undefined && !isObject(value.default_params)) throw new Error('invalid default_params')
  if (value.output_fields !== undefined && (!Array.isArray(value.output_fields) || value.output_fields.some((field) => {
    if (!isObject(field) || typeof field.key !== 'string' || !field.key.trim()) return true
    if (!['string', 'number', 'boolean', 'object', 'array'].includes(String(field.type))) return true
    for (const key of ['description', 'label', 'default']) {
      if (field[key] !== undefined && typeof field[key] !== 'string') return true
    }
    for (const key of ['required', 'enum']) {
      if (field[key] !== undefined && typeof field[key] !== 'boolean') return true
    }
    return (field.options !== undefined && !Array.isArray(field.options)) ||
      (field.properties !== undefined && !isObject(field.properties))
  }))) throw new Error('invalid output_fields')
  if (value.sort_order !== undefined && (typeof value.sort_order !== 'number' || !Number.isSafeInteger(value.sort_order))) throw new Error('invalid sort_order')
  if (value.scheduling_enabled !== undefined && typeof value.scheduling_enabled !== 'boolean') throw new Error('invalid scheduling_enabled')
  if (value.result_type !== undefined && value.result_type !== 'image' && value.result_type !== 'video') throw new Error('invalid result_type')
  return modelIntroToUpsert(value as unknown as UpsertModelIntroRequest)
}

export function serializeModelIntros(rows: ModelIntro[]): string {
  return JSON.stringify({ version: 1, exported_at: new Date().toISOString(), items: rows.map(modelIntroToUpsert) }, null, 2)
}
