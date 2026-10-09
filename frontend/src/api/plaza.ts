/**
 * Public Pricing Plaza API endpoints.
 *
 * 这些端点公开未授权可访问，对应后端 GET /api/v1/plaza/{models,plans}。
 * 其中 `models` 接口的 query 参数已在后端校验：`q` ≤ 64 字符。
 */

import type { RechargeBonusTier } from '@/types/payment'
import { apiClient } from './client'

// ==================== Types ====================

/** Currency context shared across both plaza endpoints. */
export interface PlazaCurrencyMeta {
  /** 1 CNY recharge → multiplier USD credited; used for CNY ⇄ USD on-the-fly conversion. */
  balance_recharge_multiplier: number
  /** Native currency for model rows (always "USD"). */
  model_native: 'USD'
  /** Native currency for plan rows (always "CNY"). */
  plan_native: 'CNY'
}

/** Three-tier image generation prices, all in USD per image. */
export interface PlazaImagePrices {
  tier_1k: number
  tier_2k: number
  tier_4k: number
}

/**
 * One row on the model plaza.
 *
 * `type === "token"` rows populate the `*_per_mtok` fields (USD per million tokens);
 * `type === "image"` rows populate the `*_image_prices` fields and leave token fields as 0.
 */
export interface PlazaModelRow {
  group_id: number
  group_name: string
  platform: string
  model: string
  type: 'token' | 'image'

  input_price_per_mtok?: number
  output_price_per_mtok?: number
  site_input_price_per_mtok?: number
  site_output_price_per_mtok?: number

  /**
   * Single-tier (5 minute) cache prices, USD per million tokens. Mirrors the
   * backend's `*float64`/`omitempty` semantics: the field is **omitted** when
   * the model has no cache pricing data and does not declare
   * `SupportsCacheBreakdown`. Frontend MUST render `—` for an absent value
   * and `$0` for an explicit zero (only emitted when the model declares
   * cache breakdown).
   */
  cache_write_price_per_mtok?: number
  cache_read_price_per_mtok?: number
  site_cache_write_price_per_mtok?: number
  site_cache_read_price_per_mtok?: number

  base_image_prices?: PlazaImagePrices
  site_image_prices?: PlazaImagePrices

  multiplier: number
  /** 整数百分比；例如 20 表示 20% off。 */
  discount_percent: number
}

/** One group bundled by a subscription plan. */
export interface PlazaPlanGroup {
  group_id: number
  group_name: string
  platform: string
  rate_multiplier: number
}

/** One subscription-plan card on the plaza. */
export interface PlazaPlanCard {
  id: number
  name: string
  description: string
  /** CNY (per `currency_meta.plan_native`). */
  price: number
  original_price?: number
  validity_days: number
  validity_unit: string
  features: string
  /** 主分组（groups 首元素），保留给徽标配色等需要单一代表分组的场景。 */
  group_id: number
  group_name: string
  platform: string
  /** Current default multiplier of the associated group. */
  rate_multiplier: number
  /** 套餐打包授予的全部分组；购买后每个分组各发放一条订阅。 */
  groups?: PlazaPlanGroup[]
  /** Up to 50 model names; if more exist, exposed via `models_overflow`. */
  models: string[]
  models_overflow: number
}

export interface PlazaModelsResponse {
  rows: PlazaModelRow[]
  currency_meta: PlazaCurrencyMeta
}

export interface PlazaPlansResponse {
  cards: PlazaPlanCard[]
  currency_meta: PlazaCurrencyMeta
}

// ==================== Public Recharge Promo ====================

/**
 * Active recharge promotion returned by `GET /api/v1/plaza/recharge-promo`.
 *
 * Mirrors `dto.PublicRechargePromoDTO`: derived from the recharge bonus tiers
 * configuration and only present while the tiers are non-empty and inside the
 * validity window. `version` is the same red-dot dismiss-key fingerprint the
 * authenticated checkout-info returns as `recharge_bonus_version`.
 */
export interface PublicRechargePromo {
  /** bonus: extra credit (`bonus_percent` is the bonus %); discount: `bonus_percent` is the discount %. */
  mode: 'bonus' | 'discount'
  valid_from?: string
  valid_until?: string
  /** Ascending by `min_amount` (payment currency). */
  tiers: RechargeBonusTier[]
  version: string
}

/** Top-level response shape: `promo` is `null` when no campaign is active. */
export interface PublicRechargePromoResponse {
  promo: PublicRechargePromo | null
}

// ==================== Filter Params ====================

export interface PlazaModelsFilter {
  group_id?: number
  platform?: string
  q?: string
}

// ==================== API Surface ====================

export const plazaAPI = {
  /**
   * Fetch the model plaza rows.
   *
   * @param filter optional client-side filters; the backend interprets:
   *   - `group_id`: exact match
   *   - `platform`: case-insensitive exact match
   *   - `q`: case-insensitive substring on model name (≤ 64 chars; longer is rejected with 400)
   */
  async listModels(filter: PlazaModelsFilter = {}): Promise<PlazaModelsResponse> {
    const params: Record<string, unknown> = {}
    if (filter.group_id !== undefined) params.group_id = filter.group_id
    if (filter.platform) params.platform = filter.platform
    if (filter.q) params.q = filter.q
    const { data } = await apiClient.get<PlazaModelsResponse>('/plaza/models', { params })
    return data
  },

  /** Fetch the subscription-plan plaza cards. */
  async listPlans(): Promise<PlazaPlansResponse> {
    const { data } = await apiClient.get<PlazaPlansResponse>('/plaza/plans')
    return data
  },

  /**
   * Fetch the active recharge promo (anonymous-accessible).
   *
   * Resolves to `{ promo: null }` whenever no campaign is currently active —
   * including expired windows, empty tiers, and backend errors (the backend
   * silent-skips on its end so anonymous marketing surfaces never 5xx). The
   * response is intentionally HEADER-FREE: callers MUST NOT attach an
   * `Authorization` token, mirroring `/plaza/plans` behavior.
   */
  async getRechargePromo(): Promise<PublicRechargePromoResponse> {
    const { data } = await apiClient.get<PublicRechargePromoResponse>('/plaza/recharge-promo')
    return data
  },
}

export default plazaAPI
