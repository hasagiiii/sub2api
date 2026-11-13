-- Different packages may cover the same group within an organization.
-- Keep standalone grants unique separately from package entitlements.
DROP INDEX IF EXISTS idx_org_subscriptions_org_group_active;

CREATE UNIQUE INDEX idx_org_subscriptions_org_group_standalone
    ON organization_subscriptions(organization_id, group_id)
    WHERE deleted_at IS NULL AND plan_id IS NULL;

CREATE UNIQUE INDEX idx_org_subscriptions_org_group_plan
    ON organization_subscriptions(organization_id, group_id, plan_id)
    WHERE deleted_at IS NULL AND plan_id IS NOT NULL;
