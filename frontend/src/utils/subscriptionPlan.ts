import type { SubscriptionPlan } from '@/types/payment'

/**
 * 订阅套餐支持绑定多个分组（打包授予）：购买后发放【一条】覆盖全部分组的订阅，
 * 这些分组共享该套餐的一份限额。
 *
 * 后端同时返回权威的 `group_ids` 与"主分组" `group_id`（数组首元素）。迁移期内
 * 可能出现 `group_ids` 未回填而 `group_id` 有效的存量套餐，因此前端读取分组一律
 * 经这里收口，避免各处重复写回退分支、把"未回填"误显示成"没有分组"。
 */
export function planGroupIds(plan: Pick<SubscriptionPlan, 'group_id' | 'group_ids'> | null | undefined): number[] {
  if (!plan) return []
  if (plan.group_ids?.length) return plan.group_ids
  return plan.group_id ? [plan.group_id] : []
}

/**
 * 套餐是否覆盖了指定分组。用于续费判定与 `?group=<id>` 深链筛选：单分组时代这两处
 * 直接比较 `group_id`，多分组下必须改为"包含"判断，否则绑定了该分组的套餐会被漏掉。
 */
export function planCoversGroup(
  plan: Pick<SubscriptionPlan, 'group_id' | 'group_ids'> | null | undefined,
  groupId: number
): boolean {
  if (!groupId) return false
  return planGroupIds(plan).includes(groupId)
}
