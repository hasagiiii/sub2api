import type { VideoModelPricingItem } from '@/api/videoModels'

export function formatMediaPricing(price: VideoModelPricingItem): string {
  const value = price.unit_price ?? price.price_per_second
  return Number.isFinite(value) ? `$${value.toFixed(4)}` : '-'
}

export function mediaPricingUnitKey(price: VideoModelPricingItem): string {
  switch (price.billing_mode) {
    case 'image': return 'videoModels.playground.pricingUnitImage'
    case 'per_request': return 'videoModels.playground.pricingUnitRequest'
    default: return 'videoModels.playground.pricingUnitSecond'
  }
}

export function mediaPricingLabel(price: VideoModelPricingItem): string {
  return price.quality ? `${price.resolution} / ${price.quality}` : price.resolution
}

export function mediaPricingKey(price: VideoModelPricingItem): string {
  return `${price.billing_mode ?? 'video'}:${price.resolution}:${price.quality ?? ''}`
}
