import { describe, expect, it } from 'vitest'
import { resolveImageResults } from '@/components/video/imageResults'

const first = { url: 'https://cdn.test/1.png', name: 'First', size: '1024x1024', z_index: 0 }
const second = { url: 'https://cdn.test/2.png', name: 'Second', z_index: 1 }

describe('playground image result resolution', () => {
  it.each(['images[*].url', 'images[0].url', 'images[].url', 'images[\\*].url', 'images.url', 'images', 'images[*]'])(
    'collects the actual images for %s and preserves their metadata', (path) => {
      expect(resolveImageResults({ images: [first, second], unrelated: { url: 'ignored' } }, path))
        .toEqual([{ url: first.url, metadata: first }, { url: second.url, metadata: second }])
    }
  )
  it('resolves nested array fields, skipping missing or empty images', () => {
    const payload = { batches: [{ images: [first, null, { url: '' }] }, { images: [second] }] }
    expect(resolveImageResults(payload, 'batches[0].images[0].url').map(image => image.url))
      .toEqual([first.url, second.url])
  })
  it('supports URL arrays and array-valued result fields', () => {
    expect(resolveImageResults({ output: { urls: [first.url, '', null, second.url] } }, 'output.urls').map(image => image.url))
      .toEqual([first.url, second.url])
    expect(resolveImageResults({ images: [{ url: [first.url, second.url] }] }, 'images[0].url').map(image => image.url))
      .toEqual([first.url, second.url])
  })
  it('does not discover unrelated image URLs when the configured result is missing', () => {
    expect(resolveImageResults({ thumbnail: first, images: [] }, 'images[0].url')).toEqual([])
    expect(resolveImageResults({ thumbnail: first }, 'output.images')).toEqual([])
  })
  it('uses actual data to distinguish a single image and an array, keeping literal numeric object keys', () => {
    expect(resolveImageResults({ image: first }, 'image.url')).toEqual([{ url: first.url, metadata: first }])
    expect(resolveImageResults({ images: [first] }, 'images[0].url')).toHaveLength(1)
    expect(resolveImageResults({ images: { '0': first, '1': second } }, 'images[0].url'))
      .toEqual([{ url: first.url, metadata: first }])
  })
  it('retains duplicate URLs because separate result items can carry different metadata', () => {
    expect(resolveImageResults({ images: [first, { ...second, url: first.url }] }, 'images')).toHaveLength(2)
  })
  it.each([null, undefined, {}, { images: [null, false, 0, {}, { url: '' }] }])('handles absent and invalid images: %j', payload => {
    expect(resolveImageResults(payload, 'images')).toEqual([])
    expect(resolveImageResults(payload, '')).toEqual([])
  })
})
