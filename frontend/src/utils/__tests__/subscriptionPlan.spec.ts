import { describe, expect, it } from 'vitest'

import { planCoversGroup, planGroupIds } from '../subscriptionPlan'

describe('planGroupIds', () => {
  it('returns the full bundle when group_ids is present', () => {
    expect(planGroupIds({ group_id: 7, group_ids: [7, 8, 9] })).toEqual([7, 8, 9])
  })

  it('falls back to the primary group for plans not yet backfilled', () => {
    // 迁移期：应用已上线但回填迁移未跑，group_ids 为空而 group_id 有效。
    // 回退缺失会让管理端把套餐显示成"没有分组"。
    expect(planGroupIds({ group_id: 7, group_ids: [] })).toEqual([7])
    expect(planGroupIds({ group_id: 7 })).toEqual([7])
  })

  it('returns an empty list when there is nothing to show', () => {
    expect(planGroupIds(null)).toEqual([])
    expect(planGroupIds(undefined)).toEqual([])
    expect(planGroupIds({ group_id: 0 })).toEqual([])
  })
})

describe('planCoversGroup', () => {
  it('matches any bundled group, not just the primary one', () => {
    // 续费判定与 ?group= 深链都依赖这一点：单分组时代只比较 group_id，
    // 多分组下绑定了该分组的套餐必须同样命中。
    const plan = { group_id: 7, group_ids: [7, 8] }
    expect(planCoversGroup(plan, 7)).toBe(true)
    expect(planCoversGroup(plan, 8)).toBe(true)
    expect(planCoversGroup(plan, 9)).toBe(false)
  })

  it('still matches legacy single-group plans', () => {
    expect(planCoversGroup({ group_id: 7 }, 7)).toBe(true)
  })

  it('never matches a missing group id', () => {
    expect(planCoversGroup({ group_id: 7, group_ids: [7] }, 0)).toBe(false)
    expect(planCoversGroup(null, 7)).toBe(false)
  })
})
