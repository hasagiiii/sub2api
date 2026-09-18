package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserSubscription holds the schema definition for the UserSubscription entity.
type UserSubscription struct {
	ent.Schema
}

func (UserSubscription) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_subscriptions"},
	}
}

func (UserSubscription) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (UserSubscription) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		// group_id 是订阅的主分组：等于所覆盖分组集合的首个元素。订阅可同时
		// 覆盖多个分组（见 user_subscription_groups 关联表），但展示链路与存量
		// 读取方仍需要一个确定的代表分组，因此保留并与集合同步维护。
		field.Int64("group_id"),
		// 来源套餐。限额从该套餐【实时】读取，使管理员的调整立即对已购订阅生
		// 效；NULL 表示后台手动分配、不属于任何套餐的订阅，仍按分组限额走。
		// 它同时决定唯一性语义：套餐订阅按 (user_id, plan_id) 唯一，手动分配按
		// (user_id, group_id) 唯一（见迁移 245）。
		field.Int64("plan_id").
			Optional().
			Nillable(),

		field.Time("starts_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("status").
			MaxLen(20).
			Default(domain.SubscriptionStatusActive),

		field.Time("daily_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("weekly_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("monthly_window_start").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),

		field.Float("daily_usage_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).
			Default(0),
		field.Float("weekly_usage_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).
			Default(0),
		field.Float("monthly_usage_usd").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).
			Default(0),

		field.Int64("assigned_by").
			Optional().
			Nillable(),
		field.Time("assigned_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}

func (UserSubscription) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("subscriptions").
			Field("user_id").
			Unique().
			Required(),
		edge.From("group", Group.Type).
			Ref("subscriptions").
			Field("group_id").
			Unique().
			Required(),
		edge.From("assigned_by_user", User.Type).
			Ref("assigned_subscriptions").
			Field("assigned_by").
			Unique(),
		edge.To("usage_logs", UsageLog.Type),
	}
}

func (UserSubscription) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("group_id"),
		index.Fields("status"),
		index.Fields("expires_at"),
		// 活跃订阅查询复合索引（线上由 SQL 迁移创建部分索引，schema 仅用于模型可读性对齐）
		index.Fields("user_id", "status", "expires_at"),
		index.Fields("assigned_by"),
		// 唯一性由迁移 245 的两个部分唯一索引实现，按订阅来源拆分：
		//   - 套餐订阅 (user_id, plan_id) WHERE plan_id IS NOT NULL
		//     同一套餐一条，保证重复购买走续期而不是再开一个额度池
		//   - 手动分配 (user_id, group_id) WHERE plan_id IS NULL
		//     与迁移 016 语义一致
		// 两者都带 WHERE deleted_at IS NULL，支持软删除后重新订阅。
		// 注意：不能再有全局的 (user_id, group_id) 唯一约束——两个都包含同一
		// 分组的套餐必须能被同一用户同时持有。
		index.Fields("user_id", "group_id"),
		index.Fields("plan_id"),
		index.Fields("deleted_at"),
	}
}
