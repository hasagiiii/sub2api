-- 订阅套餐支持多分组（打包授予）。
--
-- 语义：一个套餐可绑定 N 个分组，购买后为其中每个分组各发放一条订阅
-- （个人 user_subscriptions / 企业 organization_subscriptions）。
--
-- 兼容策略：新增 JSONB 数组列，原单值列保留为"主分组"（数组首元素）。
-- 存量行回填为单元素数组，读取方另有代码兜底（空数组回退到单值列），
-- 因此本迁移与应用版本的上线顺序无强耦合。

-- 1) 套餐：绑定的全部分组
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS group_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN subscription_plans.group_ids IS
    '套餐授予的全部分组 ID（打包授予）；group_id 为其首个元素，作为展示用主分组';

UPDATE subscription_plans
SET group_ids = jsonb_build_array(group_id)
WHERE group_ids = '[]'::jsonb
  AND group_id > 0;

-- 2) 订单：下单时的分组快照（套餐后续变更不影响已下单的履约范围）
ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS subscription_group_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN payment_orders.subscription_group_ids IS
    '下单时套餐绑定的全部分组 ID 快照；subscription_group_id 为其首个元素';

UPDATE payment_orders
SET subscription_group_ids = jsonb_build_array(subscription_group_id)
WHERE subscription_group_ids = '[]'::jsonb
  AND subscription_group_id IS NOT NULL;

-- 3) 成本中心权益快照：一单多分组 → 每个分组一条权益行。
--    原 order_id UNIQUE 会让第二个分组的快照直接冲突，改为 (order_id, group_id) 唯一。
--    group_id 可空，NULL 在唯一索引里互不冲突，故用 COALESCE 收敛到 0；
--    应用侧 ON CONFLICT 必须使用同样的表达式。
ALTER TABLE cost_center_subscription_entitlements
    DROP CONSTRAINT IF EXISTS cost_center_subscription_entitlements_order_id_key;

DROP INDEX IF EXISTS cost_center_subscription_entitlements_order_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS cost_center_subscription_entitlements_order_group_uniq
    ON cost_center_subscription_entitlements (order_id, COALESCE(group_id, 0));
