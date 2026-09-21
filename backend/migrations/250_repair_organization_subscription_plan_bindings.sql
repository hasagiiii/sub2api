-- Repair enterprise package assignments written by the first implementation of
-- the admin "assign by plan" flow. That implementation recorded the audit event
-- but did not persist organization_subscriptions.plan_id, leaving the groups
-- looking like independent subscriptions in the admin list and at billing time.
--
-- The audit row is the authoritative association because it contains both the
-- created subscription id and the selected plan id. Only NULL bindings are
-- repaired, so a later explicit reassignment is never overwritten.
WITH plan_assignments AS (
    SELECT DISTINCT ON ((metadata->>'subscription_id')::BIGINT)
        (metadata->>'subscription_id')::BIGINT AS subscription_id,
        (metadata->>'plan_id')::BIGINT AS plan_id
    FROM organization_audit_events
    WHERE action = 'organization.subscription.admin_assign_plan'
      AND result = 'success'
      AND (metadata->>'subscription_id') ~ '^[1-9][0-9]*$'
      AND (metadata->>'plan_id') ~ '^[1-9][0-9]*$'
    ORDER BY (metadata->>'subscription_id')::BIGINT, created_at DESC, id DESC
)
UPDATE organization_subscriptions s
SET plan_id = a.plan_id,
    updated_at = NOW()
FROM plan_assignments a
JOIN subscription_plans p ON p.id = a.plan_id
WHERE s.id = a.subscription_id
  AND s.plan_id IS NULL
  AND s.deleted_at IS NULL;

-- Paid enterprise plan orders created before the audit-based flow can also be
-- recovered from their immutable payment-order note. Keep this deliberately
-- limited to an exact subscription row note and a completed/paid order so a
-- manually assigned group is never guessed to belong to a package.
WITH payment_assignments AS (
    SELECT DISTINCT ON (s.id)
        s.id AS subscription_id,
        o.plan_id
    FROM organization_subscriptions s
    JOIN payment_orders o
      ON o.organization_id = s.organization_id
     AND o.plan_id IS NOT NULL
     AND s.notes LIKE '%payment order ' || o.id::TEXT || '%'
    JOIN subscription_plans p ON p.id = o.plan_id
    WHERE s.plan_id IS NULL
      AND s.deleted_at IS NULL
      AND o.status IN ('PAID', 'COMPLETED', 'RECHARGING')
      AND (
          o.subscription_group_id = s.group_id
          OR o.subscription_group_ids @> jsonb_build_array(s.group_id)
      )
    ORDER BY s.id, COALESCE(o.completed_at, o.paid_at, o.created_at) DESC, o.id DESC
)
UPDATE organization_subscriptions s
SET plan_id = a.plan_id,
    updated_at = NOW()
FROM payment_assignments a
WHERE s.id = a.subscription_id
  AND s.plan_id IS NULL
  AND s.deleted_at IS NULL;

-- Rebuild the shared pool for repaired rows. This preserves any usage that was
-- recorded while the rows were incorrectly treated as independent counters.
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
