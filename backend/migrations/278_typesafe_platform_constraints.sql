-- Re-admit TypeSafe after 247_repair_platform_check_constraints.sql rebuilt the
-- platform allowlists without it. Superset of 247, so existing rows validate.
-- TypeSafe 不进入渠道监控 provider，channel_monitors 相关约束保持 247 的定义。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro',
                        'grok', 'fal', 'leonardo', 'atlascloud', 'apiz',
                        'higgsfield', 'kimi', 'zhipu', 'deepseek', 'minimax',
                        'bytedance', 'opencode_go', 'typesafe'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro',
                               'grok', 'fal', 'leonardo', 'atlascloud', 'apiz',
                               'higgsfield', 'kimi', 'zhipu', 'deepseek', 'minimax',
                               'bytedance', 'opencode_go', 'typesafe'));
