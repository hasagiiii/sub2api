-- Retire Excel/BPS configuration without rewriting released migrations.
-- Preserve test history, but stop obsolete plans and remove their policy ownership.
DELETE FROM account_quality_states
 WHERE state->>'action' = 'enable_bps' OR plan_id IN (SELECT id FROM scheduled_test_plans
 WHERE pelican_config->'quality'->>'action' = 'enable_bps'
    OR pelican_config->>'test_channel' = 'bps');
UPDATE scheduled_test_plans
 SET enabled = false, next_run_at = NULL,
     pelican_config = (pelican_config - 'quality' - 'test_channel'), updated_at = NOW()
 WHERE pelican_config->'quality'->>'action' = 'enable_bps'
    OR pelican_config->>'test_channel' = 'bps';
UPDATE quality_rule_templates
 SET enabled = false, pelican_config = (pelican_config - 'quality' - 'test_channel'), updated_at = NOW()
 WHERE pelican_config->'quality'->>'action' = 'enable_bps'
    OR pelican_config->>'test_channel' = 'bps';
DELETE FROM settings WHERE position('excel_bps' in key) > 0;
UPDATE accounts a SET extra = COALESCE((SELECT jsonb_object_agg(k, v)
 FROM jsonb_each(a.extra) AS entry(k,v) WHERE position('excel_bps' in k) = 0), '{}'::jsonb)
 WHERE jsonb_typeof(extra) = 'object' AND EXISTS
 (SELECT 1 FROM jsonb_object_keys(a.extra) k WHERE position('excel_bps' in k) > 0);
DROP INDEX IF EXISTS scheduled_test_quality_account_scope_unique;
CREATE UNIQUE INDEX scheduled_test_quality_account_scope_unique
 ON scheduled_test_plans (account_id,
   (CASE WHEN pelican_config->'quality'->>'action' = 'observe_only'
     THEN 'observation' ELSE 'quarantine' END))
 WHERE pelican_config->'quality' IS NOT NULL;

-- Automatic account defaults were nested in a JSON settings value.
UPDATE settings SET value = (value::jsonb - 'excel_bps')::text, updated_at = NOW()
 WHERE key = 'smart_ops_oauth_auto_config' AND value <> '';

-- Priority scheduling no longer balances traffic across the retired BPS protocol.
UPDATE settings SET value = (value::jsonb - 'balance_protocols')::text, updated_at = NOW()
 WHERE key = 'priority_scheduling_v1' AND value <> '';

-- Policies that changed actions may still contain obsolete BPS options.
UPDATE scheduled_test_plans SET pelican_config = pelican_config #- '{quality,bps}'
 WHERE (pelican_config->'quality') ? 'bps';
UPDATE quality_rule_templates SET pelican_config = pelican_config #- '{quality,bps}'
 WHERE (pelican_config->'quality') ? 'bps';
