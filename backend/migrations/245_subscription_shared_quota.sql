-- 订阅套餐共享限额：一份额度池覆盖套餐绑定的全部分组。
--
-- 背景：限额原本只定义在 groups 上，用量计数器在 user_subscriptions 行上。
-- 迁移 244 让套餐可绑定多个分组后，履约按"每个分组各发一条订阅"进行，于是
-- 买一份套餐会得到 N 条各自独立的订阅、N 份互不相干的用量计数器——用户花一
-- 份钱拿到了 N 倍额度。
--
-- 本迁移把限额上移到套餐，并让一次购买只产生一条覆盖多分组的订阅：该订阅持
-- 有唯一的用量计数器，其覆盖的所有分组共用这一份额度。

-- 1) 套餐级限额。
--    NULL 表示套餐未设置限额，判定时回退到分组自身的 *_limit_usd，因此存量
--    套餐与后台手动分配的订阅行为完全不变。
--    与 groups 的限额列保持相同的 DECIMAL(20,8) 精度，避免比较时出现精度差。
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS daily_limit_usd DECIMAL(20, 8),
    ADD COLUMN IF NOT EXISTS weekly_limit_usd DECIMAL(20, 8),
    ADD COLUMN IF NOT EXISTS monthly_limit_usd DECIMAL(20, 8);

COMMENT ON COLUMN subscription_plans.daily_limit_usd IS
    '套餐日限额（USD）。NULL 表示未设置，回退到分组自身限额；该额度由套餐绑定的全部分组共享';

-- 2) 订阅回指来源套餐。
--    管理员调整套餐限额需要立即对已购订阅生效，所以判定时回查套餐而不是在购
--    买时快照限额。
--    NULL = 后台手动分配、不属于任何套餐的订阅，继续按分组限额走。
ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL;

COMMENT ON COLUMN user_subscriptions.plan_id IS
    '来源订阅套餐；限额从该套餐实时读取。NULL 表示后台手动分配的订阅，按分组限额走';

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_plan_id
    ON user_subscriptions (plan_id)
    WHERE plan_id IS NOT NULL AND deleted_at IS NULL;

-- 3) 订阅覆盖的分组集合。
--    这里用关联表而不是 JSONB 数组：认证热路径需要按 group_id 反查"这个分组
--    由哪条订阅覆盖"，关联表上的普通索引比 JSONB 包含查询更快也更好维护。
CREATE TABLE IF NOT EXISTS user_subscription_groups (
    subscription_id BIGINT      NOT NULL REFERENCES user_subscriptions (id) ON DELETE CASCADE,
    group_id        BIGINT      NOT NULL REFERENCES groups (id) ON DELETE CASCADE,
    -- 保持套餐 group_ids 的顺序：首个是主分组，回退时也按此顺序尝试
    sort_order      INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (subscription_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_subscription_groups_group
    ON user_subscription_groups (group_id);

-- 回填：存量订阅各覆盖自己原本的那一个分组。
INSERT INTO user_subscription_groups (subscription_id, group_id, sort_order)
SELECT id, group_id, 0
FROM user_subscriptions
WHERE deleted_at IS NULL
ON CONFLICT DO NOTHING;

-- 4) 唯一约束按来源拆分。
--    原约束 (user_id, group_id) WHERE deleted_at IS NULL 表达"一个用户在一个
--    分组下只有一条订阅"。允许不同套餐的额度池并存后这条不再成立：两个都含
--    分组 A 的套餐必须能被同一用户同时持有。
--
--    但直接删掉会连"重复购买同一套餐应当续期而非新开一个池子"也一并丢掉，
--    所以改为按来源拆成两个部分唯一索引：
--      - 套餐订阅：(user_id, plan_id) 唯一 —— 同一套餐一条，重复购买走续期
--      - 手动分配：(user_id, group_id) 唯一 —— 语义与迁移 016 完全一致
ALTER TABLE user_subscriptions
    DROP CONSTRAINT IF EXISTS user_subscriptions_user_id_group_id_key;
DROP INDEX IF EXISTS user_subscriptions_user_group_unique_active;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_plan_unique_active
    ON user_subscriptions (user_id, plan_id)
    WHERE deleted_at IS NULL AND plan_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_unique_manual
    ON user_subscriptions (user_id, group_id)
    WHERE deleted_at IS NULL AND plan_id IS NULL;

-- 5) API Key 绑定订阅，取代绑定分组。
--    一条订阅覆盖多个分组，且允许两个套餐都含同一个分组，因此"这次消费扣哪
--    个额度池"无法再由分组推断出来。让 Key 直接绑订阅即可消除该歧义：绑定的
--    订阅就是额度池，它覆盖的分组就是这把 Key 的可路由范围。
--
--    与 organization_subscription_id（迁移 196）保持一致，不加外键：认证热路
--    径按快照读取，订阅行被清理时由应用层判定失效并给出明确错误，比让 FK 静
--    默置空更容易排查。
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS user_subscription_id BIGINT;

COMMENT ON COLUMN api_keys.user_subscription_id IS
    '绑定的个人订阅（user_subscriptions.id）。设置后该 Key 消费这条订阅的额度池，可路由到它覆盖的全部分组；NULL 表示按 group_id 绑定单个分组的传统 Key';

CREATE INDEX IF NOT EXISTS idx_api_keys_user_subscription_id
    ON api_keys (user_subscription_id)
    WHERE user_subscription_id IS NOT NULL;
