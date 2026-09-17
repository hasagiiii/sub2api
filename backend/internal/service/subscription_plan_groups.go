package service

import dbent "github.com/Wei-Shaw/sub2api/ent"

// 订阅套餐多分组（打包授予）的读取收口。
//
// 数据形态：套餐与订单都同时持有"分组数组"与"单值主分组"两列。数组是权威来源，
// 单值列等于数组首元素，保留给展示链路（平台徽标/倍率/限额需要一个确定的代表
// 分组）与存量数据。
//
// 迁移期内可能出现数组为空而单值列有效的行（应用先上线、回填迁移后执行），因此
// 所有读取方都必须经由这里的函数取分组，而不是直接读字段：函数会在数组为空时回
// 退到单值列，避免把"未回填"误判成"套餐没有任何分组"从而静默不发放订阅。

// normalizeGroupIDs 清洗分组 ID 列表：丢弃非正数、按首次出现顺序去重。
//
// 顺序有意义：首个元素会被写回单值主分组列，决定结账页与广场卡片展示哪个分组的
// 平台与倍率，因此不能排序打乱管理员配置的顺序。
func normalizeGroupIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// PlanGroupIDs 返回套餐授予的全部分组，空数组的存量行回退到主分组。
func PlanGroupIDs(plan *dbent.SubscriptionPlan) []int64 {
	if plan == nil {
		return nil
	}
	if ids := normalizeGroupIDs(plan.GroupIds); len(ids) > 0 {
		return ids
	}
	if plan.GroupID > 0 {
		return []int64{plan.GroupID}
	}
	return nil
}

// PlanPrimaryGroupID 返回套餐的展示用主分组（分组数组首元素）。
func PlanPrimaryGroupID(plan *dbent.SubscriptionPlan) int64 {
	ids := PlanGroupIDs(plan)
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}

// OrderSubscriptionGroupIDs 返回订单下单时快照的全部分组，空数组的存量订单回退到
// 单值快照列。履约与退款都以此为准，而不是重新去读套餐——套餐可能已被管理员改动
// 或删除，已付款订单的履约范围必须固定在下单那一刻。
func OrderSubscriptionGroupIDs(o *dbent.PaymentOrder) []int64 {
	if o == nil {
		return nil
	}
	if ids := normalizeGroupIDs(o.SubscriptionGroupIds); len(ids) > 0 {
		return ids
	}
	if o.SubscriptionGroupID != nil && *o.SubscriptionGroupID > 0 {
		return []int64{*o.SubscriptionGroupID}
	}
	return nil
}
