export interface ImageResult {
  url: string
  metadata?: Record<string, unknown>
}

/** Resolve the configured image field only; array indices identify a field in each item. */
export function resolveImageResults(payload: unknown, path: string): ImageResult[] {
  const normalized = path.trim().replace(/\[\\\*\]/g, '[*]').replace(/\[\]/g, '[*]')
  if (!normalized) return []
  const tokens = Array.from(normalized.matchAll(/([^.[\]]+)|\[([^\]]+)\]/g), match => match[1] ?? match[2])
  const results: ImageResult[] = []

  function collect(value: unknown, metadata?: Record<string, unknown>) {
    if (Array.isArray(value)) {
      value.forEach(item => collect(item))
    } else if (typeof value === 'string' && value.trim()) {
      results.push({ url: value.trim(), metadata })
    } else if (value && typeof value === 'object') {
      const record = value as Record<string, unknown>
      if (typeof record.url === 'string' && record.url.trim()) {
        results.push({ url: record.url.trim(), metadata: record })
      }
    }
  }

  function walk(value: unknown, index: number, parent?: Record<string, unknown>) {
    if (index === tokens.length) {
      collect(value, parent)
      return
    }
    const token = tokens[index]
    if (Array.isArray(value)) {
      // Only expand indices when the actual result at this level is an array.
      const next = token === '*' || /^\d+$/.test(token) ? index + 1 : index
      value.forEach(item => walk(item, next))
    } else if (value && typeof value === 'object') {
      const record = value as Record<string, unknown>
      if (token === '*') {
        Object.values(record).forEach(item => walk(item, index + 1, record))
      } else if (Object.prototype.hasOwnProperty.call(record, token)) {
        walk(record[token], index + 1, record)
      }
    }
  }

  walk(payload, 0)
  return results
}
