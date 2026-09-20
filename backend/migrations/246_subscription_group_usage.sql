-- Track the usage of each group covered by a subscription separately from the
-- subscription-level counters. The subscription row remains the package pool;
-- this table is the group pool used when a package has no package-level limit.
CREATE TABLE IF NOT EXISTS user_subscription_group_usages (
    subscription_id      BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE CASCADE,
    group_id             BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    daily_window_start   TIMESTAMPTZ,
    weekly_window_start  TIMESTAMPTZ,
    monthly_window_start TIMESTAMPTZ,
    daily_usage_usd      DECIMAL(20, 10) NOT NULL DEFAULT 0,
    weekly_usage_usd     DECIMAL(20, 10) NOT NULL DEFAULT 0,
    monthly_usage_usd    DECIMAL(20, 10) NOT NULL DEFAULT 0,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (subscription_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_subscription_group_usages_group
    ON user_subscription_group_usages (group_id);

-- Create rows for existing covered groups and preserve the current window
-- anchors. Historical usage is reconstructed from usage_logs below.
INSERT INTO user_subscription_group_usages (
    subscription_id, group_id,
    daily_window_start, weekly_window_start, monthly_window_start
)
SELECT usg.subscription_id,
       usg.group_id,
       us.daily_window_start,
       us.weekly_window_start,
       us.monthly_window_start
FROM user_subscription_groups usg
JOIN user_subscriptions us ON us.id = usg.subscription_id
ON CONFLICT (subscription_id, group_id) DO NOTHING;

-- A few legacy rows may predate the covered-group backfill. Their primary group
-- still needs a dedicated usage row so the new independent counter is enabled.
INSERT INTO user_subscription_group_usages (
    subscription_id, group_id,
    daily_window_start, weekly_window_start, monthly_window_start
)
SELECT us.id,
       us.group_id,
       us.daily_window_start,
       us.weekly_window_start,
       us.monthly_window_start
FROM user_subscriptions us
WHERE us.deleted_at IS NULL
ON CONFLICT (subscription_id, group_id) DO NOTHING;

-- Backfill each group's current-window usage from the durable usage log. The
-- package counters on user_subscriptions are intentionally left untouched.
UPDATE user_subscription_group_usages gu
SET daily_usage_usd = COALESCE(x.daily_usage_usd, 0),
    weekly_usage_usd = COALESCE(x.weekly_usage_usd, 0),
    monthly_usage_usd = COALESCE(x.monthly_usage_usd, 0),
    updated_at = NOW()
FROM (
    SELECT ul.subscription_id,
           ul.group_id,
           COALESCE(SUM(ul.actual_cost) FILTER (
               WHERE us.daily_window_start IS NOT NULL AND ul.created_at >= us.daily_window_start
           ), 0) AS daily_usage_usd,
           COALESCE(SUM(ul.actual_cost) FILTER (
               WHERE us.weekly_window_start IS NOT NULL AND ul.created_at >= us.weekly_window_start
           ), 0) AS weekly_usage_usd,
           COALESCE(SUM(ul.actual_cost) FILTER (
               WHERE us.monthly_window_start IS NOT NULL AND ul.created_at >= us.monthly_window_start
           ), 0) AS monthly_usage_usd
    FROM usage_logs ul
    JOIN user_subscriptions us ON us.id = ul.subscription_id
    WHERE ul.subscription_id IS NOT NULL
      AND ul.group_id IS NOT NULL
    GROUP BY ul.subscription_id, ul.group_id
) x
WHERE gu.subscription_id = x.subscription_id
  AND gu.group_id = x.group_id;
