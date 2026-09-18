package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionPlan holds the schema definition for the SubscriptionPlan entity.
//
// 删除策略：硬删除
// SubscriptionPlan 使用硬删除而非软删除，原因如下：
//   - 套餐为管理员维护的商品配置，删除即表示下架移除
//   - 通过 for_sale 字段控制是否在售，删除仅用于彻底移除
//   - 已购买的订阅记录保存在 UserSubscription 中，不受套餐删除影响
type SubscriptionPlan struct {
	ent.Schema
}

func (SubscriptionPlan) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "subscription_plans"},
	}
}

func (SubscriptionPlan) Fields() []ent.Field {
	return []ent.Field{
		// group_id 保留为"主分组"：等于 group_ids 的首个元素。
		// 展示链路（结账页/广场卡片的平台徽标、倍率、限额）需要一个确定的代表
		// 分组，且存量数据与旧客户端仍按单分组读取，因此写入时与 group_ids 同步
		// 维护，不做废弃。
		field.Int64("group_id"),
		// group_ids 是套餐授予的全部分组（打包授予）：购买后为其中每个分组各发放
		// 一条订阅。空数组表示尚未回填的存量行，读取方一律经 PlanGroupIDs 兜底回
		// 退到 group_id，避免迁移期出现"套餐没有任何分组"。
		field.JSON("group_ids", []int64{}).
			Default([]int64{}).
			SchemaType(map[string]string{dialect.Postgres: "jsonb"}),
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("description").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.Float("price").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}),
		field.Int64("standard_quota_tokens").
			Default(0),
		// 套餐级限额：由 group_ids 里的全部分组【共享】这一份额度，而不是每个
		// 分组各给一份。NULL 表示套餐未设限额，判定时回退到分组自身的
		// *_limit_usd，因此存量套餐与后台手动分配的订阅行为不变。
		// 判定时实时回查（不在购买时快照），管理员调整后立即对已购订阅生效。
		field.Float("daily_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Optional().
			Nillable(),
		field.Float("weekly_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Optional().
			Nillable(),
		field.Float("monthly_limit_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Optional().
			Nillable(),
		field.Float("original_price").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,2)"}).
			Optional().
			Nillable(),
		field.String("currency").
			MaxLen(3).
			Default(""),
		field.Int("validity_days").
			Default(30),
		field.String("validity_unit").
			MaxLen(10).
			Default("day"),
		field.String("features").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Default(""),
		field.String("product_name").
			MaxLen(100).
			Default(""),
		field.Bool("for_sale").
			Default(true),
		field.Int("sort_order").
			Default(0),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (SubscriptionPlan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("group_id"),
		index.Fields("for_sale"),
	}
}
