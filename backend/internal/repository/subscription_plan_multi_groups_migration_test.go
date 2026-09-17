package repository

import (
	"strings"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
)

// 订阅套餐多分组迁移必须是幂等的加列 + 回填，且不能破坏存量数据。
func TestSubscriptionPlanMultiGroupsMigrationIsAdditiveAndIdempotent(t *testing.T) {
	sql, err := dbmigrations.FS.ReadFile("244_subscription_plan_multi_groups.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(sql)

	for _, required := range []string{
		"ALTER TABLE subscription_plans\n    ADD COLUMN IF NOT EXISTS group_ids JSONB",
		"ALTER TABLE payment_orders\n    ADD COLUMN IF NOT EXISTS subscription_group_ids JSONB",
		// 存量行必须回填成单元素数组，否则升级后老套餐会被读成"没有分组"。
		"UPDATE subscription_plans",
		"UPDATE payment_orders",
		// 一单多分组要为每个分组各记一条权益行，order_id 单列唯一必须放开。
		"DROP CONSTRAINT IF EXISTS cost_center_subscription_entitlements_order_id_key",
		"CREATE UNIQUE INDEX IF NOT EXISTS cost_center_subscription_entitlements_order_group_uniq",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing %q", required)
		}
	}

	// 唯一索引与 cost_center_repo.go 的 ON CONFLICT 表达式必须完全一致，
	// 否则 Postgres 找不到冲突目标而直接报错。group_id 可空，NULL 在唯一索引里
	// 互不冲突，因此必须经 COALESCE 收敛。
	if !strings.Contains(text, "(order_id, COALESCE(group_id, 0))") {
		t.Fatal("unique index must key on (order_id, COALESCE(group_id, 0)) to match the repo ON CONFLICT")
	}

	lowered := strings.ToLower(text)
	// 保留单值列作为主分组：删列会让未升级的实例与回滚彻底失败。
	for _, forbidden := range []string{
		"drop column",
		"delete from subscription_plans",
		"delete from payment_orders",
	} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("migration must stay additive, found %q", forbidden)
		}
	}
}
