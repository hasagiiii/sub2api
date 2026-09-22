package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// normalizePlanCurrency validates and normalizes the display-only currency label.
// Empty means "no label" and is kept as-is so existing plans stay unchanged.
func normalizePlanCurrency(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	currency, err := payment.NormalizePaymentCurrency(raw)
	if err != nil {
		return "", infraerrors.BadRequest("PLAN_CURRENCY_INVALID", "currency must be a 3-letter ISO currency code")
	}
	return currency, nil
}

// validatePlanRequired checks that all required fields for a plan are provided.
func validatePlanRequired(name string, groupIDs []int64, price float64, validityDays int, validityUnit string, originalPrice *float64) error {
	if strings.TrimSpace(name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if len(groupIDs) == 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "at least one group is required")
	}
	// 调用方通常已经过 normalizeGroupIDs，这里仍逐个校验：本函数是套餐必填项的
	// 唯一收口，不能依赖调用方一定做了清洗。
	for _, id := range groupIDs {
		if id <= 0 {
			return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "group id must be > 0")
		}
	}
	if price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if validityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if strings.TrimSpace(validityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if originalPrice != nil && *originalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// validatePlanPatch validates only the non-nil fields in a patch update.
func validatePlanPatch(req UpdatePlanRequest) error {
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		return infraerrors.BadRequest("PLAN_NAME_REQUIRED", "plan name is required")
	}
	if groupIDs, touched := req.ResolvedGroupIDs(); touched && len(groupIDs) == 0 {
		return infraerrors.BadRequest("PLAN_GROUP_REQUIRED", "at least one group is required")
	}
	if req.Price != nil && *req.Price <= 0 {
		return infraerrors.BadRequest("PLAN_PRICE_INVALID", "price must be > 0")
	}
	if req.ValidityDays != nil && *req.ValidityDays <= 0 {
		return infraerrors.BadRequest("PLAN_VALIDITY_REQUIRED", "validity days must be > 0")
	}
	if req.ValidityUnit != nil && strings.TrimSpace(*req.ValidityUnit) == "" {
		return infraerrors.BadRequest("PLAN_VALIDITY_UNIT_REQUIRED", "validity unit is required")
	}
	if req.OriginalPrice != nil && *req.OriginalPrice < 0 {
		return infraerrors.BadRequest("PLAN_ORIGINAL_PRICE_INVALID", "original price must be >= 0")
	}
	return nil
}

// validatePlanGroupsExist 校验套餐绑定的每个分组都存在、启用且为订阅型。
//
// 多分组下这一步不可省：单分组时代下单前才校验（validateSubOrder），管理员配错
// 只会在用户付款时暴露；打包授予里任何一个分组不合法都会让整笔订单履约失败，
// 必须在保存套餐时就拦住。
func (s *PaymentConfigService) validatePlanGroupsExist(ctx context.Context, groupIDs []int64) error {
	if len(groupIDs) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(groupIDs...)).All(ctx)
	if err != nil {
		return fmt.Errorf("load plan groups: %w", err)
	}
	found := make(map[int64]*dbent.Group, len(groups))
	for _, g := range groups {
		found[int64(g.ID)] = g
	}
	for _, id := range groupIDs {
		g, ok := found[id]
		if !ok {
			return infraerrors.BadRequest("PLAN_GROUP_NOT_FOUND",
				fmt.Sprintf("group %d does not exist", id))
		}
		if g.Status != payment.EntityStatusActive {
			return infraerrors.BadRequest("PLAN_GROUP_INACTIVE",
				fmt.Sprintf("group %d is not active", id))
		}
		if g.SubscriptionType != SubscriptionTypeSubscription {
			return infraerrors.BadRequest("PLAN_GROUP_TYPE_MISMATCH",
				fmt.Sprintf("group %d is not a subscription type", id))
		}
	}
	return nil
}

// --- Plan CRUD ---

// PlanGroupInfo holds the group details needed for subscription plan display.
type PlanGroupInfo struct {
	Platform           string   `json:"platform"`
	Name               string   `json:"name"`
	RateMultiplier     float64  `json:"rate_multiplier"`
	PeakRateEnabled    bool     `json:"peak_rate_enabled"`
	PeakStart          string   `json:"peak_start"`
	PeakEnd            string   `json:"peak_end"`
	PeakRateMultiplier float64  `json:"peak_rate_multiplier"`
	DailyLimitUSD      *float64 `json:"daily_limit_usd"`
	WeeklyLimitUSD     *float64 `json:"weekly_limit_usd"`
	MonthlyLimitUSD    *float64 `json:"monthly_limit_usd"`
	ModelScopes        []string `json:"supported_model_scopes"`
}

// GetGroupInfoMap returns a map of group_id → PlanGroupInfo for the given plans.
// 覆盖每个套餐绑定的所有分组，而非仅主分组。
func (s *PaymentConfigService) GetGroupInfoMap(ctx context.Context, plans []*dbent.SubscriptionPlan) map[int64]PlanGroupInfo {
	ids := make([]int64, 0, len(plans))
	seen := make(map[int64]bool)
	for _, p := range plans {
		for _, gid := range PlanGroupIDs(p) {
			if !seen[gid] {
				seen[gid] = true
				ids = append(ids, gid)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	groups, err := s.entClient.Group.Query().Where(group.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil
	}
	m := make(map[int64]PlanGroupInfo, len(groups))
	for _, g := range groups {
		m[int64(g.ID)] = PlanGroupInfo{
			Platform:           g.Platform,
			Name:               g.Name,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			DailyLimitUSD:      g.DailyLimitUsd,
			WeeklyLimitUSD:     g.WeeklyLimitUsd,
			MonthlyLimitUSD:    g.MonthlyLimitUsd,
			ModelScopes:        g.SupportedModelScopes,
		}
	}
	return m
}

func (s *PaymentConfigService) ListPlans(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) ListPlansForSale(ctx context.Context) ([]*dbent.SubscriptionPlan, error) {
	return s.entClient.SubscriptionPlan.Query().Where(subscriptionplan.ForSaleEQ(true)).Order(subscriptionplan.BySortOrder()).All(ctx)
}

func (s *PaymentConfigService) CreatePlan(ctx context.Context, req CreatePlanRequest) (*dbent.SubscriptionPlan, error) {
	groupIDs := req.ResolvedGroupIDs()
	if err := validatePlanRequired(req.Name, groupIDs, req.Price, req.ValidityDays, req.ValidityUnit, req.OriginalPrice); err != nil {
		return nil, err
	}
	if err := s.validatePlanGroupsExist(ctx, groupIDs); err != nil {
		return nil, err
	}
	currency, err := normalizePlanCurrency(req.Currency)
	if err != nil {
		return nil, err
	}
	b := s.entClient.SubscriptionPlan.Create().
		// 数组与主分组同步写入：主分组恒等于数组首元素。
		SetGroupIds(groupIDs).SetGroupID(groupIDs[0]).
		SetName(req.Name).SetDescription(req.Description).
		SetPrice(req.Price).SetCurrency(currency).SetValidityDays(req.ValidityDays).SetValidityUnit(req.ValidityUnit).
		SetFeatures(req.Features).SetProductName(req.ProductName).
		SetForSale(req.ForSale).SetSortOrder(req.SortOrder)
	if req.OriginalPrice != nil {
		b.SetOriginalPrice(*req.OriginalPrice)
	}
	// 套餐级共享限额：<= 0 视为未设置（与分组限额语义一致），不写入该列。
	if positivePlanLimit(req.DailyLimitUSD) {
		b.SetDailyLimitUsd(*req.DailyLimitUSD)
	}
	if positivePlanLimit(req.WeeklyLimitUSD) {
		b.SetWeeklyLimitUsd(*req.WeeklyLimitUSD)
	}
	if positivePlanLimit(req.MonthlyLimitUSD) {
		b.SetMonthlyLimitUsd(*req.MonthlyLimitUSD)
	}
	return b.Save(ctx)
}

// positivePlanLimit 报告该限额是否为"已设置"。
// <= 0 与 nil 一样表示不限额，这与 Group.HasDailyLimit 的判定保持一致。
func positivePlanLimit(v *float64) bool { return v != nil && *v > 0 }

// UpdatePlan updates a subscription plan by ID (patch semantics).
// NOTE: This function exceeds 30 lines due to per-field nil-check patch update boilerplate
// plus a validation guard for non-nil fields.
func (s *PaymentConfigService) UpdatePlan(ctx context.Context, id int64, req UpdatePlanRequest) (*dbent.SubscriptionPlan, error) {
	if err := validatePlanPatch(req); err != nil {
		return nil, err
	}
	u := s.entClient.SubscriptionPlan.UpdateOneID(id)
	var syncedGroupIDs []int64
	if groupIDs, touched := req.ResolvedGroupIDs(); touched {
		if err := s.validatePlanGroupsExist(ctx, groupIDs); err != nil {
			return nil, err
		}
		u.SetGroupIds(groupIDs).SetGroupID(groupIDs[0])
		syncedGroupIDs = groupIDs
	}
	if req.Name != nil {
		u.SetName(*req.Name)
	}
	if req.Description != nil {
		u.SetDescription(*req.Description)
	}
	if req.Price != nil {
		u.SetPrice(*req.Price)
	}
	if req.OriginalPrice != nil {
		u.SetOriginalPrice(*req.OriginalPrice)
	}
	if req.Currency != nil {
		currency, err := normalizePlanCurrency(*req.Currency)
		if err != nil {
			return nil, err
		}
		u.SetCurrency(currency)
	}
	if req.ValidityDays != nil {
		u.SetValidityDays(*req.ValidityDays)
	}
	if req.ValidityUnit != nil {
		u.SetValidityUnit(*req.ValidityUnit)
	}
	if req.Features != nil {
		u.SetFeatures(*req.Features)
	}
	if req.ProductName != nil {
		u.SetProductName(*req.ProductName)
	}
	if req.ForSale != nil {
		u.SetForSale(*req.ForSale)
	}
	if req.SortOrder != nil {
		u.SetSortOrder(*req.SortOrder)
	}
	// 限额补丁：nil 不修改；传 <= 0 表示清除。套餐仍处于模式时，清除的窗口为不限额。
	if req.DailyLimitUSD != nil {
		if *req.DailyLimitUSD > 0 {
			u.SetDailyLimitUsd(*req.DailyLimitUSD)
		} else {
			u.ClearDailyLimitUsd()
		}
	}
	if req.WeeklyLimitUSD != nil {
		if *req.WeeklyLimitUSD > 0 {
			u.SetWeeklyLimitUsd(*req.WeeklyLimitUSD)
		} else {
			u.ClearWeeklyLimitUsd()
		}
	}
	if req.MonthlyLimitUSD != nil {
		if *req.MonthlyLimitUSD > 0 {
			u.SetMonthlyLimitUsd(*req.MonthlyLimitUSD)
		} else {
			u.ClearMonthlyLimitUsd()
		}
	}
	plan, err := u.Save(ctx)
	if err != nil {
		return nil, err
	}
	if len(syncedGroupIDs) > 0 {
		if err := s.syncAssignedPlanGroups(ctx, id, syncedGroupIDs); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (s *PaymentConfigService) DeletePlan(ctx context.Context, id int64) error {
	count, err := s.countPendingOrdersByPlan(ctx, id)
	if err != nil {
		return fmt.Errorf("check pending orders: %w", err)
	}
	if count > 0 {
		return infraerrors.Conflict("PENDING_ORDERS",
			fmt.Sprintf("this plan has %d in-progress orders and cannot be deleted — wait for orders to complete first", count))
	}
	return s.entClient.SubscriptionPlan.DeleteOneID(id).Exec(ctx)
}

// GetPlan returns a subscription plan by ID.
func (s *PaymentConfigService) GetPlan(ctx context.Context, id int64) (*dbent.SubscriptionPlan, error) {
	plan, err := s.entClient.SubscriptionPlan.Get(ctx, id)
	if err != nil {
		return nil, infraerrors.NotFound("PLAN_NOT_FOUND", "subscription plan not found")
	}
	return plan, nil
}

func (s *PaymentConfigService) syncAssignedPlanGroups(ctx context.Context, planID int64, groupIDs []int64) error {
	if s == nil || s.sqlDB == nil || planID <= 0 || len(groupIDs) == 0 {
		return nil
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM user_subscriptions WHERE plan_id=$1 AND deleted_at IS NULL`, planID)
	if err != nil {
		return err
	}
	var subscriptionIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		subscriptionIDs = append(subscriptionIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, subscriptionID := range subscriptionIDs {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_subscription_groups WHERE subscription_id=$1`, subscriptionID); err != nil {
			return err
		}
		for i, groupID := range groupIDs {
			if groupID <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_subscription_groups(subscription_id, group_id, sort_order) VALUES($1,$2,$3) ON CONFLICT (subscription_id, group_id) DO UPDATE SET sort_order=EXCLUDED.sort_order`, subscriptionID, groupID, i); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO user_subscription_group_usages(subscription_id, group_id) VALUES($1,$2) ON CONFLICT (subscription_id, group_id) DO NOTHING`, subscriptionID, groupID); err != nil {
				return err
			}
		}
	}

	orgRows, err := tx.QueryContext(ctx, `SELECT DISTINCT organization_id FROM organization_subscriptions WHERE plan_id=$1 AND deleted_at IS NULL AND status='active' AND expires_at>NOW()`, planID)
	if err != nil {
		return err
	}
	var organizationIDs []int64
	for orgRows.Next() {
		var id int64
		if err := orgRows.Scan(&id); err != nil {
			_ = orgRows.Close()
			return err
		}
		organizationIDs = append(organizationIDs, id)
	}
	if err := orgRows.Err(); err != nil {
		_ = orgRows.Close()
		return err
	}
	_ = orgRows.Close()
	for _, organizationID := range organizationIDs {
		var startsAt, expiresAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT MIN(starts_at), MAX(expires_at) FROM organization_subscriptions WHERE organization_id=$1 AND plan_id=$2 AND deleted_at IS NULL AND status='active' AND expires_at>NOW()`, organizationID, planID).Scan(&startsAt, &expiresAt); err != nil {
			return err
		}
		for _, groupID := range groupIDs {
			if groupID <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO organization_subscriptions(organization_id,group_id,plan_id,starts_at,expires_at,status,assigned_at)
				VALUES($1,$2,$3,$4,$5,'active',NOW())
				ON CONFLICT (organization_id, group_id) WHERE deleted_at IS NULL DO UPDATE SET
					plan_id=EXCLUDED.plan_id,
					status='active',
					expires_at=GREATEST(organization_subscriptions.expires_at, EXCLUDED.expires_at),
					updated_at=NOW()
				WHERE organization_subscriptions.plan_id IS NULL OR organization_subscriptions.plan_id=EXCLUDED.plan_id`, organizationID, groupID, planID, startsAt, expiresAt); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
