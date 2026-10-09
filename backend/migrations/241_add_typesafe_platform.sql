-- Add TypeSafe (Jev System One) as a first-class platform.
--
-- 1. user_platform_quotas.platform CHECK
-- 2. composite_model_routes.target_platform CHECK
--
-- TypeSafe 不是对话模型，不进入渠道监控 provider，因此 channel_monitors /
-- channel_monitor_request_templates 的约束保持不变。
--
-- Runs after 238_opencode_go_platform.sql. DROP ... IF EXISTS 保证可重入；
-- 新约束是 238 的超集（保留本分支的 kiro/fal/leonardo/atlascloud/apiz/
-- higgsfield/bytedance），存量行瞬时校验通过。
-- 247_repair_platform_check_constraints.sql 会再次重建约束且不含 typesafe，
-- 因此 278_typesafe_platform_constraints.sql 负责在其之后补回 typesafe。

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
