-- Enterprise subscriptions need the same package quota semantics as personal
-- subscriptions. Each covered group remains a bindable row, while plan_id
-- identifies rows that share one package-level usage pool.

ALTER TABLE organization_subscriptions
    ADD COLUMN IF NOT EXISTS plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_org_subscriptions_org_plan_active
    ON organization_subscriptions(organization_id, plan_id)
    WHERE deleted_at IS NULL AND plan_id IS NOT NULL;

COMMENT ON COLUMN organization_subscriptions.plan_id IS
    '来源套餐；套餐配置了任一额度时，同一 organization_id + plan_id 的分组行共用套餐额度；NULL 表示按分组独立限额';
