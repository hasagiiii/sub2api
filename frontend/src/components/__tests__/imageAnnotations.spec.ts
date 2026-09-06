import { describe, expect, it } from 'vitest'
import { annotationPrompt, normalizeAnnotation, readAnnotationDocument, replaceAnnotationPrompt, serializeImageReferences, type AnnotationDocument } from '../video/imageAnnotations'
import { mapToRows, rowsToMap } from '../common/paramSchemaRow'
import { extractFieldSpecs } from '../video/paramSpec'

describe('image annotations', () => {
  it('omits empty references and serializes single and multiple images', () => {
    expect(JSON.stringify({ image: serializeImageReferences([]) })).toBe('{}')
    expect(serializeImageReferences(['https://example.com/a.png'])).toBe('https://example.com/a.png')
    expect(serializeImageReferences(['a', 'b'])).toEqual(['a', 'b'])
    expect(serializeImageReferences('https://example.com/a.png')).toBe('https://example.com/a.png')
  })
  const url = 'https://example.com/a.png'
  const document: AnnotationDocument = { [url]: [
    { id: 'box', kind: 'box', points: [800, 900, 100, 200], instruction: 'Remove text' },
    { id: 'arrow', kind: 'arrow', points: [10, 20, 500, 600], instruction: 'Move object' },
  ] }
  it('normalizes boxes and preserves arrow direction', () => {
    const prompt = annotationPrompt([url], document)
    expect(prompt).toContain('<bbox>100 200 800 900</bbox>')
    expect(prompt).toContain('arrow from (10, 20) to (500, 600)')
    expect(prompt).toContain('Image 1')
    expect(normalizeAnnotation({ id: 'point', kind: 'point', points: [-3, 1200], instruction: '' }).points).toEqual([0, 1000])
  })
  it('replaces generated text instead of appending duplicates', () => {
    const generated = annotationPrompt([url], document)
    const once = replaceAnnotationPrompt('Original instructions', '', generated)
    expect(replaceAnnotationPrompt(once, generated, generated)).toBe(once)
    expect(replaceAnnotationPrompt(once, generated, '')).toBe('Original instructions')
  })
  it('ignores annotations for removed images and invalid history', () => {
    expect(annotationPrompt([], document)).toBe('')
    expect(readAnnotationDocument({ [url]: [{ kind: 'box', points: [] }] })).toEqual({ [url]: [] })
    expect(readAnnotationDocument(document)[url][0].points).toEqual([100, 200, 800, 900])
  })
  it('round trips the data-driven widget and reference limit', () => {
    const schema = { image: { items: { value: '', widget: 'image' }, widget: 'image-annotations', maxItems: 10, prompt_field: 'edit_prompt' } }
    const roundTrip = rowsToMap(mapToRows(schema))
    const field = extractFieldSpecs(roundTrip)[0]
    expect(field.widget).toBe('image-annotations')
    expect(field.promptField).toBe('edit_prompt')
    expect(field.maxItems).toBe(10)
  })
})
