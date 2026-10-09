-- 平台白名单改由应用层的平台清单（internal/domain/platforms.go）统一校验。
-- 242_drop_platform_check_constraints.sql 已删除这两条约束，但 fork 的
-- 247_repair_platform_check_constraints.sql 在其后又重建了它们，这里再次删除，
-- 使全新库与已有库最终都不带数据库层的平台白名单。
--
-- channel_monitors / channel_monitor_request_templates 的 provider CHECK 表示
-- 渠道监控已实现的探测能力，不属于平台清单，保留不动。
--
-- DROP ... IF EXISTS 保证可重入。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
