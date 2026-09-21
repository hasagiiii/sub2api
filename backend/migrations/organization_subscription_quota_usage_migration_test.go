package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrganizationSubscriptionQuotaUsageMigrationCreatesSharedPool(t *testing.T) {
	sqlBytes, err := FS.ReadFile("249_organization_subscription_quota_usage.sql")
	require.NoError(t, err)
	sql := string(sqlBytes)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS organization_subscription_plan_usages")
	require.Contains(t, sql, "PRIMARY KEY (organization_id, plan_id)")
	require.Contains(t, sql, "ON CONFLICT (organization_id, plan_id) DO NOTHING")
	require.Contains(t, strings.ToLower(sql), "sum(daily_usage_usd)")
}

func TestOrganizationSubscriptionPlanBindingRepairMigrationUsesExplicitEvidence(t *testing.T) {
	sqlBytes, err := FS.ReadFile("250_repair_organization_subscription_plan_bindings.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(sqlBytes))
	require.Contains(t, sql, "organization.subscription.admin_assign_plan")
	require.Contains(t, sql, "metadata->>'subscription_id'")
	require.Contains(t, sql, "metadata->>'plan_id'")
	require.Contains(t, sql, "payment order '")
	require.Contains(t, sql, "organization_subscription_plan_usages")
	// The migration must never overwrite a binding that was explicitly repaired
	// or assigned later.
	require.Contains(t, sql, "s.plan_id is null")
}
