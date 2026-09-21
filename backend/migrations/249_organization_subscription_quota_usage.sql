-- Keep enterprise package usage separate from each covered group's usage.
-- The subscription row counters remain the per-group counters; this table is
-- the shared package counter used whenever the package has any positive limit.
CREATE TABLE IF NOT EXISTS organization_subscription_plan_usages (
    organization_id      BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    plan_id              BIGINT NOT NULL REFERENCES subscription_plans(id) ON DELETE CASCADE,
    daily_window_start   TIMESTAMPTZ,
    weekly_window_start  TIMESTAMPTZ,
    monthly_window_start TIMESTAMPTZ,
    daily_usage_usd      DECIMAL(20, 10) NOT NULL DEFAULT 0,
    weekly_usage_usd     DECIMAL(20, 10) NOT NULL DEFAULT 0,
    monthly_usage_usd    DECIMAL(20, 10) NOT NULL DEFAULT 0,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, plan_id)
);

CREATE INDEX IF NOT EXISTS idx_org_subscription_plan_usages_plan
    ON organization_subscription_plan_usages (plan_id);

-- Preserve usage already recorded on enterprise subscription rows. Before this
-- migration those counters were the only durable representation available.
INSERT INTO organization_subscription_plan_usages (
    organization_id, plan_id,
    daily_window_start, weekly_window_start, monthly_window_start,
    daily_usage_usd, weekly_usage_usd, monthly_usage_usd
)
SELECT organization_id,
       plan_id,
       MIN(daily_window_start),
       MIN(weekly_window_start),
       MIN(monthly_window_start),
       COALESCE(SUM(daily_usage_usd), 0),
       COALESCE(SUM(weekly_usage_usd), 0),
       COALESCE(SUM(monthly_usage_usd), 0)
FROM organization_subscriptions
WHERE plan_id IS NOT NULL
  AND deleted_at IS NULL
GROUP BY organization_id, plan_id
ON CONFLICT (organization_id, plan_id) DO NOTHING;
