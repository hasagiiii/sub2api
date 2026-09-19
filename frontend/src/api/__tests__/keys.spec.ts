import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, put } = vi.hoisted(() => ({
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { post, put },
}))

import { create, update } from '@/api/keys'

describe('API key fallback group payloads', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    post.mockResolvedValue({ data: {} })
    put.mockResolvedValue({ data: {} })
  })

  it('serializes ordered fallback groups for a personal key', async () => {
    await create('ordered', 1, undefined, [], [], 0, undefined, undefined, null, [3, 2])

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'ordered',
      group_id: 1,
      fallback_group_ids: [3, 2],
    })
  })

  it('serializes ordered fallback groups for an enterprise key', async () => {
    await create('enterprise', 1, undefined, [], [], 0, undefined, undefined, 90, [3, 2])

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'enterprise',
      organization_subscription_id: 90,
      fallback_group_ids: [3, 2],
    })
  })

  it('serializes company balance preference when enabled', async () => {
    await create('company-first', 1, undefined, [], [], 0, undefined, undefined, null, [], true)

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'company-first',
      group_id: 1,
      fallback_group_ids: [],
      prefer_company_balance: true,
    })
  })

  it('preserves ordered fallback groups on update', async () => {
    await update(7, { group_id: 1, fallback_group_ids: [2, 3] })

    expect(put).toHaveBeenCalledWith('/keys/7', {
      group_id: 1,
      fallback_group_ids: [2, 3],
    })
  })

  // 指定扣费套餐与分组并存：分组决定路由（同一套餐里的两个分组可能提供相同模型，
  // 只有用户知道要用哪个），套餐只决定扣哪一份额度，回退分组照常生效。
  it('sends the pinned plan alongside the group and fallbacks', async () => {
    await create('plan-pinned', 1, undefined, [], [], 0, undefined, undefined, null, [3, 2], false, {
      userSubscriptionId: 55,
    })

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'plan-pinned',
      group_id: 1,
      fallback_group_ids: [3, 2],
      user_subscription_id: 55,
    })
  })

  // 企业订阅的额度池来自公司订阅，个人套餐的指定在那里没有意义。
  it('keeps the enterprise binding when both subscription kinds are supplied', async () => {
    await create('enterprise-wins', 1, undefined, [], [], 0, undefined, undefined, 90, [], false, {
      userSubscriptionId: 55,
    })

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'enterprise-wins',
      organization_subscription_id: 90,
      fallback_group_ids: [],
    })
  })
})
