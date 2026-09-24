-- 为模型介绍增加调度开关。
-- 关闭后模型不会出现在用户演练台列表，但不影响通过 API 直接调用。
ALTER TABLE model_intros
    ADD COLUMN IF NOT EXISTS scheduling_enabled BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS modelintros_scheduling_enabled_idx
    ON model_intros (scheduling_enabled);
