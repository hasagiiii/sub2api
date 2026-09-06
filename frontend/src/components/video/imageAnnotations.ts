export interface ImageAnnotation {
  id: string
  kind: 'point' | 'box' | 'arrow'
  points: number[]
  instruction: string
}

export type AnnotationDocument = Record<string, ImageAnnotation[]>

export function serializeImageReferences(value: unknown): unknown {
  if (!Array.isArray(value)) return value
  if (value.length === 0) return undefined
  return value.length === 1 ? value[0] : value
}

export function clampCoordinate(value: number): number {
  return Math.round(Math.max(0, Math.min(1000, value)))
}

export function normalizeAnnotation(annotation: ImageAnnotation): ImageAnnotation {
  const points = annotation.points.map(clampCoordinate)
  if (annotation.kind === 'box') {
    return { ...annotation, points: [Math.min(points[0], points[2]), Math.min(points[1], points[3]), Math.max(points[0], points[2]), Math.max(points[1], points[3])] }
  }
  return { ...annotation, points }
}

export function annotationPrompt(images: string[], document: AnnotationDocument): string {
  const lines: string[] = []
  images.forEach((url, index) => {
    for (const item of document[url] || []) {
      const a = normalizeAnnotation(item)
      const p = a.points
      const location = a.kind === 'box' ? `<bbox>${p.join(' ')}</bbox>` : a.kind === 'arrow' ? `arrow from (${p[0]}, ${p[1]}) to (${p[2]}, ${p[3]})` : `point (${p[0]}, ${p[1]})`
      lines.push(`Image ${index + 1}: ${location}${a.instruction.trim() ? `; ${a.instruction.trim()}` : ''}`)
    }
  })
  return lines.length ? `Coordinates are normalized to 0-1000.\n${lines.join('\n')}` : ''
}

export function replaceAnnotationPrompt(prompt: string, previous: string, next: string): string {
  const base = previous ? prompt.replace(previous, '').trim() : prompt.trim()
  return [base, next].filter(Boolean).join('\n\n')
}

export function readAnnotationDocument(value: unknown): AnnotationDocument {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  const result: AnnotationDocument = {}
  for (const [url, items] of Object.entries(value)) {
    if (!Array.isArray(items)) continue
    result[url] = items.filter((item): item is ImageAnnotation => item && typeof item.id === 'string' && ['point', 'box', 'arrow'].includes(item.kind) && typeof item.instruction === 'string' && Array.isArray(item.points) && item.points.length === (item.kind === 'point' ? 2 : 4) && item.points.every((n: unknown) => typeof n === 'number' && Number.isFinite(n))).map(normalizeAnnotation)
  }
  return result
}
