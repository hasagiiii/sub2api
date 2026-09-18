package repository

import (
	"strings"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
)

// 共享额度池迁移：加列 + 建关联表 + 拆分唯一约束，必须幂等且不丢存量数据。
func TestSubscriptionSharedQuotaMigrationIsAdditiveAndIdempotent(t *testing.T) {
	sql, err := dbmigrations.FS.ReadFile("245_subscription_shared_quota.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(sql)

	for _, required := range []string{
		// 套餐级限额，三个窗口齐全
		"ADD COLUMN IF NOT EXISTS daily_limit_usd DECIMAL(20, 8)",
		"ADD COLUMN IF NOT EXISTS weekly_limit_usd DECIMAL(20, 8)",
		"ADD COLUMN IF NOT EXISTS monthly_limit_usd DECIMAL(20, 8)",
		// 订阅回指套餐：限额需实时生效，故判定时回查而非快照
		"ADD COLUMN IF NOT EXISTS plan_id BIGINT",
		"CREATE TABLE IF NOT EXISTS user_subscription_groups",
		"CREATE INDEX IF NOT EXISTS idx_user_subscription_groups_group",
		"ADD COLUMN IF NOT EXISTS user_subscription_id BIGINT",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing %q", required)
		}
	}

	// 存量订阅必须回填关联表，否则升级后这些订阅会变成"不覆盖任何分组"而全部失效。
	if !strings.Contains(text, "INSERT INTO user_subscription_groups") ||
		!strings.Contains(text, "FROM user_subscriptions") {
		t.Fatal("migration must backfill user_subscription_groups from existing subscriptions")
	}

	// 唯一约束按来源拆分：套餐订阅按 (user_id, plan_id)，手动分配沿用 (user_id, group_id)。
	// 少了前者，重复购买同一套餐会开出第二个额度池而不是续期；
	// 少了后者，后台手动分配会退化成可以无限重复建订阅。
	for _, required := range []string{
		"user_subscriptions_user_plan_unique_active",
		"WHERE deleted_at IS NULL AND plan_id IS NOT NULL",
		"user_subscriptions_user_group_unique_manual",
		"WHERE deleted_at IS NULL AND plan_id IS NULL",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("migration missing unique-index clause %q", required)
		}
	}

	// 旧的全局唯一索引必须显式移除，否则两个都含同一分组的套餐无法并存。
	if !strings.Contains(text, "DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active") {
		t.Fatal("migration must drop the old (user_id, group_id) unique index")
	}

	lowered := strings.ToLower(text)
	// 保留既有列与数据：删列或删数据都会让回滚与未升级实例彻底失败。
	for _, forbidden := range []string{
		"drop column",
		"delete from user_subscriptions",
		"drop table user_subscriptions",
		"truncate",
	} {
		if strings.Contains(lowered, forbidden) {
			t.Fatalf("migration must stay additive, found %q", forbidden)
		}
	}
}
