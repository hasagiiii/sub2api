import { describe, expect, it } from 'vitest'
import { formatMediaPricing, mediaPricingKey, mediaPricingLabel, mediaPricingUnitKey } from '../mediaModelPricing'
import type { VideoModelPricingItem } from '@/api/videoModels'

const video: VideoModelPricingItem = { resolution: '720p', price_per_second: 0.12, currency: 'USD', enabled: true }

describe('media model pricing', () => {
  it('keeps legacy video prices in seconds', () => {
    expect(formatMediaPricing(video)).toBe('$0.1200')
    expect(mediaPricingUnitKey(video)).toBe('videoModels.playground.pricingUnitSecond')
  })

  it('displays image prices independently of the legacy per-second value', () => {
    const image = { ...video, resolution: '1K', billing_mode: 'image' as const, unit_price: 0.25, price_per_second: 0, quality: 'high' }
    expect(formatMediaPricing(image)).toBe('$0.2500')
    expect(mediaPricingUnitKey(image)).toBe('videoModels.playground.pricingUnitImage')
    expect(mediaPricingLabel(image)).toBe('1K / high')
    expect(mediaPricingKey(image)).not.toBe(mediaPricingKey({ ...image, quality: 'low' }))
  })

  it('shows per-request pricing and preserves explicit zero prices', () => {
    const price = { ...video, billing_mode: 'per_request' as const, unit_price: 0 }
    expect(formatMediaPricing(price)).toBe('$0.0000')
    expect(mediaPricingUnitKey(price)).toBe('videoModels.playground.pricingUnitRequest')
  })
})
