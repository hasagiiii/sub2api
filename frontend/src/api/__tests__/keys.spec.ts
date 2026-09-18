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

  // 绑定套餐（订阅）时候选分组完全由订阅的覆盖集合决定。同时发送 group_id /
  // fallback 会形成两套来源，后端也会忽略它们，因此这里必须只发绑定本身。
  it('sends only the subscription binding when a plan is bound', async () => {
    await create('plan-bound', 1, undefined, [], [], 0, undefined, undefined, null, [3, 2], false, {
      userSubscriptionId: 55,
    })

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'plan-bound',
      user_subscription_id: 55,
    })
  })

  // 企业订阅优先级高于个人订阅，与后端及编辑表单的取值顺序一致。
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
