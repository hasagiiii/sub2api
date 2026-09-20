package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"entgo.io/ent/dialect/sql"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userSubscriptionRepository struct {
	client *dbent.Client
}

func NewUserSubscriptionRepository(client *dbent.Client) service.UserSubscriptionRepository {
	return &userSubscriptionRepository{client: client}
}

func (r *userSubscriptionRepository) Create(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.Create().
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetNillablePlanID(sub.PlanID).
		SetExpiresAt(sub.ExpiresAt).
		SetNillableDailyWindowStart(sub.DailyWindowStart).
		SetNillableWeeklyWindowStart(sub.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(sub.MonthlyWindowStart).
		SetDailyUsageUsd(sub.DailyUsageUSD).
		SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).
		SetNillableAssignedBy(sub.AssignedBy)

	if sub.StartsAt.IsZero() {
		builder.SetStartsAt(time.Now())
	} else {
		builder.SetStartsAt(sub.StartsAt)
	}
	if sub.Status != "" {
		builder.SetStatus(sub.Status)
	}
	if !sub.AssignedAt.IsZero() {
		builder.SetAssignedAt(sub.AssignedAt)
	}
	// Keep compatibility with historical behavior: always store notes as a string value.
	builder.SetNotes(sub.Notes)

	created, err := builder.Save(ctx)
	if err == nil {
		applyUserSubscriptionEntityToService(sub, created)
		// 覆盖分组集合存在关联表里，必须与订阅行在同一事务内写入：只写了订阅
		// 却没写关联表，这条订阅就不覆盖任何分组，用户买了却一个分组都用不了。
		if err = replaceSubscriptionCoveredGroups(ctx, client, created.ID, sub.CoveredGroupIDs()); err != nil {
			return fmt.Errorf("persist subscription covered groups: %w", err)
		}
	}
	return translatePersistenceError(err, nil, service.ErrSubscriptionAlreadyExists)
}

// replaceSubscriptionCoveredGroups 全量重写一条订阅的覆盖分组集合。
//
// 先删后插而不是增量 diff：套餐分组变更后集合可能增删混合，全量重写最不容易出
// 现残留。sort_order 保留传入顺序，首个即主分组，回退时按此顺序尝试。
func replaceSubscriptionCoveredGroups(ctx context.Context, client *dbent.Client, subscriptionID int64, groupIDs []int64) error {
	if subscriptionID <= 0 {
		return nil
	}
	if _, err := client.ExecContext(ctx, `DELETE FROM user_subscription_groups WHERE subscription_id = $1`, subscriptionID); err != nil {
		return err
	}
	for i, groupID := range uniqueInt64s(groupIDs) {
		if groupID <= 0 {
			continue
		}
		if _, err := client.ExecContext(ctx,
			`INSERT INTO user_subscription_groups(subscription_id, group_id, sort_order) VALUES($1, $2, $3)
			 ON CONFLICT (subscription_id, group_id) DO UPDATE SET sort_order = EXCLUDED.sort_order`,
			subscriptionID, groupID, i); err != nil {
			return err
		}
		if _, err := client.ExecContext(ctx, `
			INSERT INTO user_subscription_group_usages(subscription_id, group_id)
			VALUES($1, $2) ON CONFLICT (subscription_id, group_id) DO NOTHING`, subscriptionID, groupID); err != nil {
			return err
		}
	}
	return nil
}

func (r *userSubscriptionRepository) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		WithAssignedByUser().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	out := userSubscriptionEntityToService(m)
	if err := r.hydrateSubscription(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userSubscriptionRepository) GetByIDForUpdate(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		ForUpdate().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	out := userSubscriptionEntityToService(m)
	if err := r.hydrateSubscription(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userSubscriptionRepository) GetByIDIncludeDeleted(ctx context.Context, id int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	queryCtx := mixins.SkipSoftDelete(ctx)
	m, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(id)).
		WithUser().
		WithGroup().
		WithAssignedByUser().
		Only(queryCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	out := userSubscriptionEntityToServicePreserveStatus(m)
	if err := r.hydrateSubscription(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetByUserIDAndGroupID 查找覆盖指定分组的订阅（不限状态）。
//
// 必须按覆盖集合查而不是按 group_id 列：一条覆盖 [A,B] 的订阅其 group_id 是主
// 分组 A，按列过滤会漏掉它在分组 B 上的覆盖关系。
func (r *userSubscriptionRepository) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	return r.findSubscriptionCoveringGroup(ctx, userID, groupID, false)
}

// GetActiveByUserIDAndGroupID 查找覆盖指定分组的【活跃】订阅。
func (r *userSubscriptionRepository) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	return r.findSubscriptionCoveringGroup(ctx, userID, groupID, true)
}

// ReplaceCoveredGroups 全量重写订阅覆盖的分组集合。
func (r *userSubscriptionRepository) ReplaceCoveredGroups(ctx context.Context, subscriptionID int64, groupIDs []int64) error {
	client := clientFromContext(ctx, r.client)
	return replaceSubscriptionCoveredGroups(ctx, client, subscriptionID, groupIDs)
}

// GetByUserIDAndPlanID 按来源套餐定位订阅，对应 (user_id, plan_id) 部分唯一索引。
func (r *userSubscriptionRepository) GetByUserIDAndPlanID(ctx context.Context, userID, planID int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.PlanIDEQ(planID),
		).
		WithGroup().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	out := userSubscriptionEntityToService(m)
	if err := r.hydrateSubscription(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetManualByUserIDAndGroupID 只查后台手动分配的订阅（plan_id IS NULL），对应
// (user_id, group_id) 部分唯一索引。
func (r *userSubscriptionRepository) GetManualByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(groupID),
			usersubscription.PlanIDIsNil(),
		).
		WithGroup().
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	out := userSubscriptionEntityToService(m)
	if err := r.hydrateSubscription(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// findSubscriptionCoveringGroup 按覆盖关系定位订阅。
//
// 允许两个套餐覆盖同一分组后，(user, group) 可能命中多条订阅。这里的选择必须
// 确定性，否则同一把 Key 的请求会在多个额度池之间漂移、账目无法对账。排序规则：
//  1. 主分组精确匹配的优先——它是最"贴合"该分组的订阅；
//  2. 其次取先到期的，让快过期的额度先被用掉，避免过期作废；
//  3. 最后按 id 兜底，保证完全确定。
//
// 绑定了订阅的 API Key 不走这里（它直接按订阅 ID 取池），因此这条路径只服务于
// 传统的按分组绑定的 Key。
func (r *userSubscriptionRepository) findSubscriptionCoveringGroup(ctx context.Context, userID, groupID int64, activeOnly bool) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)

	query := `
		SELECT us.id
		FROM user_subscriptions us
		JOIN user_subscription_groups usg ON usg.subscription_id = us.id
		WHERE us.user_id = $1
		  AND usg.group_id = $2
		  AND us.deleted_at IS NULL`
	args := []any{userID, groupID}
	if activeOnly {
		query += `
		  AND us.status = $3
		  AND us.expires_at > $4`
		args = append(args, service.SubscriptionStatusActive, time.Now())
	}
	query += `
		ORDER BY (us.group_id = $2) DESC, us.expires_at ASC, us.id ASC
		LIMIT 1`

	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var subscriptionID int64
	found := false
	func() {
		defer func() { _ = rows.Close() }()
		if rows.Next() {
			err = rows.Scan(&subscriptionID)
			found = err == nil
		}
	}()
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, service.ErrSubscriptionNotFound
	}
	return r.GetByID(ctx, subscriptionID)
}

func (r *userSubscriptionRepository) Update(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return service.ErrSubscriptionNilInput
	}

	client := clientFromContext(ctx, r.client)
	builder := client.UserSubscription.UpdateOneID(sub.ID).
		SetUserID(sub.UserID).
		SetGroupID(sub.GroupID).
		SetNillablePlanID(sub.PlanID).
		SetStartsAt(sub.StartsAt).
		SetExpiresAt(sub.ExpiresAt).
		SetStatus(sub.Status).
		SetNillableDailyWindowStart(sub.DailyWindowStart).
		SetNillableWeeklyWindowStart(sub.WeeklyWindowStart).
		SetNillableMonthlyWindowStart(sub.MonthlyWindowStart).
		SetDailyUsageUsd(sub.DailyUsageUSD).
		SetWeeklyUsageUsd(sub.WeeklyUsageUSD).
		SetMonthlyUsageUsd(sub.MonthlyUsageUSD).
		SetNillableAssignedBy(sub.AssignedBy).
		SetAssignedAt(sub.AssignedAt).
		SetNotes(sub.Notes)

	updated, err := builder.Save(ctx)
	if err == nil {
		applyUserSubscriptionEntityToService(sub, updated)
		// 覆盖分组可能随套餐配置变化，续期/改配时一并同步。GroupIDs 为空表示
		// 调用方未加载该集合，此时保持关联表原样，避免把已有覆盖关系清空。
		if len(sub.GroupIDs) > 0 {
			if err := replaceSubscriptionCoveredGroups(ctx, client, sub.ID, sub.GroupIDs); err != nil {
				return fmt.Errorf("sync subscription covered groups: %w", err)
			}
		}
		return nil
	}
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionAlreadyExists)
}

func (r *userSubscriptionRepository) Delete(ctx context.Context, id int64) error {
	// Match GORM semantics: deleting a missing row is not an error.
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.Delete().Where(usersubscription.IDEQ(id)).Exec(ctx)
	return err
}

func (r *userSubscriptionRepository) Restore(ctx context.Context, subscriptionID int64, restoredStatus string) (*service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	queryCtx := mixins.SkipSoftDelete(ctx)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetStatus(restoredStatus).
		ClearDeletedAt().
		SetUpdatedAt(time.Now()).
		Save(queryCtx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrSubscriptionNotFound, service.ErrSubscriptionRestoreConflict)
	}
	return r.GetByID(ctx, subscriptionID)
}

func (r *userSubscriptionRepository) ListByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID)).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.entitiesToServiceHydrated(ctx, subs)
}

func (r *userSubscriptionRepository) ListActiveByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.entitiesToServiceHydrated(ctx, subs)
}

func (r *userSubscriptionRepository) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query().Where(usersubscription.GroupIDEQ(groupID))

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	subs, err := q.
		WithUser().
		WithGroup().
		Order(dbent.Desc(usersubscription.FieldCreatedAt)).
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}

	hydrated, err := r.entitiesToServiceHydrated(ctx, subs)
	if err != nil {
		return nil, nil, err
	}
	return hydrated, paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	client := clientFromContext(ctx, r.client)
	q := client.UserSubscription.Query()
	includeSoftDeleted := status == "" || status == service.SubscriptionStatusRevoked
	if userID != nil {
		q = q.Where(usersubscription.UserIDEQ(*userID))
	}
	if groupID != nil {
		// 按覆盖关系筛选，而不是只比主分组：套餐订阅的 group_id 只是它覆盖的首个
		// 分组，按其余分组筛选时它同样应该出现在结果里。
		// 仍保留主分组的直接比较作为兜底，覆盖关系行缺失时行为与改动前一致。
		gid := *groupID
		q = q.Where(func(s *sql.Selector) {
			t := sql.Table("user_subscription_groups")
			s.Where(sql.Or(
				sql.EQ(s.C(usersubscription.FieldGroupID), gid),
				sql.In(
					s.C(usersubscription.FieldID),
					sql.Select(t.C("subscription_id")).From(t).Where(sql.EQ(t.C("group_id"), gid)),
				),
			))
		})
	}
	if platform != "" {
		groupPredicates := []predicate.Group{group.PlatformEQ(platform)}
		if includeSoftDeleted {
			groupPredicates = append(groupPredicates, group.DeletedAtIsNil())
		}
		q = q.Where(usersubscription.HasGroupWith(groupPredicates...))
	}

	// Status filtering with real-time expiration check
	now := time.Now()
	switch status {
	case service.SubscriptionStatusActive:
		// Active: status is active AND not yet expired
		q = q.Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(now),
		)
	case service.SubscriptionStatusExpired:
		// Expired: status is expired OR (status is active but already expired)
		q = q.Where(
			usersubscription.Or(
				usersubscription.StatusEQ(service.SubscriptionStatusExpired),
				usersubscription.And(
					usersubscription.StatusEQ(service.SubscriptionStatusActive),
					usersubscription.ExpiresAtLTE(now),
				),
			),
		)
	case service.SubscriptionStatusRevoked:
		// Revoked is a DTO/API display state backed by user_subscriptions.deleted_at.
		q = q.Where(usersubscription.DeletedAtNotNil())
	case "":
		// No filter. Use SkipSoftDelete below so admin "all status" includes revoked history.
	default:
		// Other persisted status.
		q = q.Where(usersubscription.StatusEQ(status))
	}

	queryCtx := ctx
	if includeSoftDeleted {
		queryCtx = mixins.SkipSoftDelete(ctx)
	}

	total, err := q.Clone().Count(queryCtx)
	if err != nil {
		return nil, nil, err
	}

	if !includeSoftDeleted {
		q = q.WithUser().WithGroup().WithAssignedByUser()
	}

	// Determine sort field
	var field string
	switch sortBy {
	case "expires_at":
		field = usersubscription.FieldExpiresAt
	case "status":
		field = usersubscription.FieldStatus
	default:
		field = usersubscription.FieldCreatedAt
	}

	// Determine sort order (default: desc)
	if sortOrder == "asc" && sortBy != "" {
		q = q.Order(dbent.Asc(field))
	} else {
		q = q.Order(dbent.Desc(field))
	}

	subs, err := q.
		Offset(params.Offset()).
		Limit(params.Limit()).
		All(queryCtx)
	if err != nil {
		return nil, nil, err
	}

	// 覆盖分组与套餐限额必须补齐：管理员列表要展示套餐订阅覆盖了哪些分组，用量
	// 进度也要用套餐限额做分母。漏掉会让套餐订阅看起来只覆盖主分组，且进度按主
	// 分组的限额计算而显示错误的百分比。
	result, err := r.entitiesToServiceHydrated(ctx, subs)
	if err != nil {
		return nil, nil, err
	}
	if includeSoftDeleted {
		if err := r.attachUserSubscriptionRelations(ctx, result); err != nil {
			return nil, nil, err
		}
	}

	return result, paginationResultFromTotal(int64(total), params), nil
}

func (r *userSubscriptionRepository) ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	return client.UserSubscription.Query().
		Where(usersubscription.UserIDEQ(userID), usersubscription.GroupIDEQ(groupID)).
		Exist(ctx)
}

func (r *userSubscriptionRepository) ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error) {
	return r.ExistsByUserIDAndGroupID(ctx, userID, groupID)
}

func (r *userSubscriptionRepository) ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetExpiresAt(newExpiresAt).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateStatus(ctx context.Context, subscriptionID int64, status string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetStatus(status).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserSubscription.UpdateOneID(subscriptionID).
		SetNotes(notes).
		Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ActivateWindows(ctx context.Context, id int64, dailyStart, periodicStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Update().
		Where(
			usersubscription.IDEQ(id),
			usersubscription.DailyWindowStartIsNil(),
			usersubscription.WeeklyWindowStartIsNil(),
			usersubscription.MonthlyWindowStartIsNil(),
		).
		SetDailyWindowStart(dailyStart).
		SetWeeklyWindowStart(periodicStart).
		SetMonthlyWindowStart(periodicStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	update := client.UserSubscription.UpdateOneID(id)
	if resetDaily {
		update.SetDailyUsageUsd(0).SetDailyWindowStart(dailyStart)
	}
	if resetWeekly {
		update.SetWeeklyUsageUsd(0).SetWeeklyWindowStart(periodicStart)
	}
	if resetMonthly {
		update.SetMonthlyUsageUsd(0).SetMonthlyWindowStart(periodicStart)
	}
	_, err := update.Save(ctx)
	return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
}

func (r *userSubscriptionRepository) ResetDailyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.DailyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.DailyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetDailyUsageUsd(0).
		SetDailyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetWeeklyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.WeeklyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.WeeklyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetWeeklyUsageUsd(0).
		SetWeeklyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) ResetMonthlyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	query := client.UserSubscription.Update().Where(usersubscription.IDEQ(id))
	if expectedWindowStart == nil {
		query = query.Where(usersubscription.MonthlyWindowStartIsNil())
	} else {
		query = query.Where(usersubscription.MonthlyWindowStartEQ(*expectedWindowStart))
	}
	n, err := query.
		SetMonthlyUsageUsd(0).
		SetMonthlyWindowStart(newWindowStart).
		Save(ctx)
	return r.translateConditionalWindowReset(ctx, client, id, n, err)
}

func (r *userSubscriptionRepository) translateConditionalWindowReset(ctx context.Context, client *dbent.Client, id int64, affected int, err error) error {
	if err != nil {
		return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	if affected > 0 {
		return nil
	}

	// A stale reset is an expected no-op: another request already advanced the
	// window. Preserve not-found semantics for callers that target a missing row.
	exists, err := client.UserSubscription.Query().Where(usersubscription.IDEQ(id)).Exist(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrSubscriptionNotFound, nil)
	}
	if !exists {
		return service.ErrSubscriptionNotFound
	}
	return nil
}

// IncrementUsage 原子性地累加订阅用量。
// 限额检查已在请求前由 BillingCacheService.CheckBillingEligibility 完成，
// 此处仅负责记录实际消费，确保消费数据的完整性。
func (r *userSubscriptionRepository) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`

	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, updateSQL, costUSD, id)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected > 0 {
		return nil
	}

	// affected == 0：订阅不存在或已删除
	return service.ErrSubscriptionNotFound
}

// IncrementUsageForGroup records both the package-level usage and the usage
// of the concrete group that served the request. The latter is what enforces
// group limits for plans without a package-level limit.
func (r *userSubscriptionRepository) IncrementUsageForGroup(ctx context.Context, subscriptionID, groupID int64, costUSD float64) error {
	client := clientFromContext(ctx, r.client)
	if groupID <= 0 {
		return r.IncrementUsage(ctx, subscriptionID, costUSD)
	}
	if _, err := client.ExecContext(ctx, `
		INSERT INTO user_subscription_group_usages(subscription_id, group_id)
		VALUES($1, $2) ON CONFLICT (subscription_id, group_id) DO NOTHING`, subscriptionID, groupID); err != nil {
		return err
	}
	const updateSQL = `
		UPDATE user_subscriptions us
		SET daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		WHERE us.id = $2 AND us.deleted_at IS NULL`
	result, err := client.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrSubscriptionNotFound
	}
	_, err = client.ExecContext(ctx, `
		UPDATE user_subscription_group_usages
		SET daily_usage_usd = daily_usage_usd + $1,
			weekly_usage_usd = weekly_usage_usd + $1,
			monthly_usage_usd = monthly_usage_usd + $1,
			updated_at = NOW()
		WHERE subscription_id = $2 AND group_id = $3`, costUSD, subscriptionID, groupID)
	return err
}

func (r *userSubscriptionRepository) ActivateGroupWindows(ctx context.Context, subscriptionID, groupID int64, dailyStart, periodicStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.ExecContext(ctx, `
		INSERT INTO user_subscription_group_usages(
			subscription_id, group_id, daily_window_start, weekly_window_start, monthly_window_start
		) VALUES($1, $2, $3, $4, $4)
		ON CONFLICT (subscription_id, group_id) DO UPDATE SET
			daily_window_start = COALESCE(user_subscription_group_usages.daily_window_start, EXCLUDED.daily_window_start),
			weekly_window_start = COALESCE(user_subscription_group_usages.weekly_window_start, EXCLUDED.weekly_window_start),
			monthly_window_start = COALESCE(user_subscription_group_usages.monthly_window_start, EXCLUDED.monthly_window_start)`,
		subscriptionID, groupID, dailyStart, periodicStart)
	return err
}

func (r *userSubscriptionRepository) ResetGroupUsageWindows(ctx context.Context, subscriptionID, groupID int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error {
	client := clientFromContext(ctx, r.client)
	if _, err := client.ExecContext(ctx, `
		INSERT INTO user_subscription_group_usages(subscription_id, group_id)
		VALUES($1, $2) ON CONFLICT (subscription_id, group_id) DO NOTHING`, subscriptionID, groupID); err != nil {
		return err
	}
	sets := make([]string, 0, 6)
	if resetDaily {
		sets = append(sets, "daily_usage_usd = 0", "daily_window_start = $3")
	}
	if resetWeekly {
		sets = append(sets, "weekly_usage_usd = 0", "weekly_window_start = $4")
	}
	if resetMonthly {
		sets = append(sets, "monthly_usage_usd = 0", "monthly_window_start = $4")
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = NOW()")
	_, err := client.ExecContext(ctx, fmt.Sprintf(`UPDATE user_subscription_group_usages SET %s WHERE subscription_id = $1 AND group_id = $2`, strings.Join(sets, ", ")), subscriptionID, groupID, dailyStart, periodicStart)
	return err
}

func (r *userSubscriptionRepository) ResetGroupDailyUsage(ctx context.Context, subscriptionID, groupID int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	return r.resetGroupUsageWindow(ctx, subscriptionID, groupID, "daily", expectedWindowStart, newWindowStart)
}

func (r *userSubscriptionRepository) ResetGroupWeeklyUsage(ctx context.Context, subscriptionID, groupID int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	return r.resetGroupUsageWindow(ctx, subscriptionID, groupID, "weekly", expectedWindowStart, newWindowStart)
}

func (r *userSubscriptionRepository) ResetGroupMonthlyUsage(ctx context.Context, subscriptionID, groupID int64, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	return r.resetGroupUsageWindow(ctx, subscriptionID, groupID, "monthly", expectedWindowStart, newWindowStart)
}

func (r *userSubscriptionRepository) resetGroupUsageWindow(ctx context.Context, subscriptionID, groupID int64, period string, expectedWindowStart *time.Time, newWindowStart time.Time) error {
	columns := map[string][2]string{
		"daily":   {"daily_usage_usd", "daily_window_start"},
		"weekly":  {"weekly_usage_usd", "weekly_window_start"},
		"monthly": {"monthly_usage_usd", "monthly_window_start"},
	}
	column, ok := columns[period]
	if !ok {
		return fmt.Errorf("unknown group usage period %q", period)
	}
	where := "window_start IS NULL"
	args := []any{subscriptionID, groupID, newWindowStart}
	if expectedWindowStart != nil {
		where = "window_start = $4"
		args = append(args, *expectedWindowStart)
	}
	query := fmt.Sprintf(`UPDATE user_subscription_group_usages
		SET %s = 0, %s = $3, updated_at = NOW()
		WHERE subscription_id = $1 AND group_id = $2 AND %s`, column[0], column[1], strings.Replace(where, "window_start", column[1], 1))
	result, err := clientFromContext(ctx, r.client).ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	return nil
}

func (r *userSubscriptionRepository) BatchUpdateExpiredStatus(ctx context.Context) (int64, error) {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Update().
		Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtLTE(time.Now()),
		).
		SetStatus(service.SubscriptionStatusExpired).
		Save(ctx)
	return int64(n), err
}

// Extra repository helpers (currently used only by integration tests).

func (r *userSubscriptionRepository) ListExpired(ctx context.Context) ([]service.UserSubscription, error) {
	client := clientFromContext(ctx, r.client)
	subs, err := client.UserSubscription.Query().
		Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtLTE(time.Now()),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return r.entitiesToServiceHydrated(ctx, subs)
}

func (r *userSubscriptionRepository) CountByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	count, err := client.UserSubscription.Query().Where(usersubscription.GroupIDEQ(groupID)).Count(ctx)
	return int64(count), err
}

func (r *userSubscriptionRepository) CountActiveByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	count, err := client.UserSubscription.Query().
		Where(
			usersubscription.GroupIDEQ(groupID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
		).
		Count(ctx)
	return int64(count), err
}

func (r *userSubscriptionRepository) DeleteByGroupID(ctx context.Context, groupID int64) (int64, error) {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserSubscription.Delete().Where(usersubscription.GroupIDEQ(groupID)).Exec(ctx)
	return int64(n), err
}

func (r *userSubscriptionRepository) attachUserSubscriptionRelations(ctx context.Context, subs []service.UserSubscription) error {
	if len(subs) == 0 {
		return nil
	}

	userIDs := make([]int64, 0, len(subs))
	groupIDs := make([]int64, 0, len(subs))
	assignedByIDs := make([]int64, 0, len(subs))
	for i := range subs {
		userIDs = append(userIDs, subs[i].UserID)
		groupIDs = append(groupIDs, subs[i].GroupID)
		if subs[i].AssignedBy != nil {
			assignedByIDs = append(assignedByIDs, *subs[i].AssignedBy)
		}
	}

	client := clientFromContext(ctx, r.client)
	users, err := client.User.Query().Where(user.IDIn(uniqueInt64s(userIDs)...)).All(ctx)
	if err != nil {
		return err
	}
	userByID := make(map[int64]*service.User, len(users))
	for _, u := range users {
		userByID[u.ID] = userEntityToService(u)
	}

	groups, err := client.Group.Query().Where(group.IDIn(uniqueInt64s(groupIDs)...)).All(ctx)
	if err != nil {
		return err
	}
	groupByID := make(map[int64]*service.Group, len(groups))
	for _, g := range groups {
		groupByID[g.ID] = groupEntityToService(g)
	}

	assignedByID := map[int64]*service.User{}
	if len(assignedByIDs) > 0 {
		assignedUsers, err := client.User.Query().Where(user.IDIn(uniqueInt64s(assignedByIDs)...)).All(ctx)
		if err != nil {
			return err
		}
		assignedByID = make(map[int64]*service.User, len(assignedUsers))
		for _, u := range assignedUsers {
			assignedByID[u.ID] = userEntityToService(u)
		}
	}

	for i := range subs {
		subs[i].User = userByID[subs[i].UserID]
		subs[i].Group = groupByID[subs[i].GroupID]
		if subs[i].AssignedBy != nil {
			subs[i].AssignedByUser = assignedByID[*subs[i].AssignedBy]
		}
	}
	return nil
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func userSubscriptionEntityToService(m *dbent.UserSubscription) *service.UserSubscription {
	return userSubscriptionEntityToServiceWithStatusMapping(m, true)
}

func userSubscriptionEntityToServicePreserveStatus(m *dbent.UserSubscription) *service.UserSubscription {
	return userSubscriptionEntityToServiceWithStatusMapping(m, false)
}

func userSubscriptionEntityToServiceWithStatusMapping(m *dbent.UserSubscription, mapDeletedToRevoked bool) *service.UserSubscription {
	if m == nil {
		return nil
	}
	status := m.Status
	if mapDeletedToRevoked && m.DeletedAt != nil {
		status = service.SubscriptionStatusRevoked
	}
	out := &service.UserSubscription{
		ID:                 m.ID,
		UserID:             m.UserID,
		GroupID:            m.GroupID,
		PlanID:             m.PlanID,
		StartsAt:           m.StartsAt,
		ExpiresAt:          m.ExpiresAt,
		Status:             status,
		DailyWindowStart:   m.DailyWindowStart,
		WeeklyWindowStart:  m.WeeklyWindowStart,
		MonthlyWindowStart: m.MonthlyWindowStart,
		DailyUsageUSD:      m.DailyUsageUsd,
		WeeklyUsageUSD:     m.WeeklyUsageUsd,
		MonthlyUsageUSD:    m.MonthlyUsageUsd,
		AssignedBy:         m.AssignedBy,
		AssignedAt:         m.AssignedAt,
		Notes:              derefString(m.Notes),
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
		DeletedAt:          m.DeletedAt,
	}
	if m.Edges.User != nil {
		out.User = userEntityToService(m.Edges.User)
	}
	if m.Edges.Group != nil {
		out.Group = groupEntityToService(m.Edges.Group)
	}
	if m.Edges.AssignedByUser != nil {
		out.AssignedByUser = userEntityToService(m.Edges.AssignedByUser)
	}
	return out
}

// hydrateSubscription 补齐订阅的额度池信息：覆盖分组集合与来源套餐限额。
//
// 这两项决定限额判定结果，必须在所有返回订阅的路径上补齐；漏掉会让判定退化成
// 只看主分组，多分组套餐的共享额度就形同虚设。
func (r *userSubscriptionRepository) hydrateSubscription(ctx context.Context, sub *service.UserSubscription) error {
	if sub == nil {
		return nil
	}
	return r.hydrateSubscriptions(ctx, []*service.UserSubscription{sub})
}

// hydrateSubscriptions 批量补齐，避免在认证热路径上产生 N+1 查询。
func (r *userSubscriptionRepository) hydrateSubscriptions(ctx context.Context, subs []*service.UserSubscription) error {
	if len(subs) == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)

	subIDs := make([]int64, 0, len(subs))
	planIDs := make([]int64, 0, len(subs))
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		subIDs = append(subIDs, sub.ID)
		if sub.PlanID != nil && *sub.PlanID > 0 {
			planIDs = append(planIDs, *sub.PlanID)
		}
	}

	groupsBySub, err := loadCoveredGroupsBySubscription(ctx, client, uniqueInt64s(subIDs))
	if err != nil {
		return err
	}
	allGroupIDs := make([]int64, 0)
	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if groupIDs := groupsBySub[sub.ID]; len(groupIDs) > 0 {
			allGroupIDs = append(allGroupIDs, groupIDs...)
		} else {
			allGroupIDs = append(allGroupIDs, sub.GroupID)
		}
	}
	groupLimitsByID, err := loadGroupLimits(ctx, client, uniqueInt64s(allGroupIDs))
	if err != nil {
		return err
	}
	groupUsagesBySub, err := loadGroupUsages(ctx, client, uniqueInt64s(subIDs))
	if err != nil {
		return err
	}
	planDetailsByID, err := loadPlanDetails(ctx, client, uniqueInt64s(planIDs))
	if err != nil {
		return err
	}

	for _, sub := range subs {
		if sub == nil {
			continue
		}
		if covered := groupsBySub[sub.ID]; len(covered) > 0 {
			sub.GroupIDs = covered
		} else {
			// 关联表尚未回填（迁移期）或订阅刚建好：回退到主分组，绝不留空，
			// 否则这条订阅会被判定成"不覆盖任何分组"而完全不可用。
			sub.GroupIDs = []int64{sub.GroupID}
		}
		sub.GroupNames = make([]string, 0, len(sub.GroupIDs))
		sub.GroupLimits = make([]service.SubscriptionGroupLimit, 0, len(sub.GroupIDs))
		sub.GroupUsages = groupUsagesBySub[sub.ID]
		for _, groupID := range sub.GroupIDs {
			groupLimit := groupLimitsByID[groupID]
			if groupLimit.GroupID == 0 {
				groupLimit.GroupID = groupID
			}
			sub.GroupNames = append(sub.GroupNames, groupLimit.Name)
			sub.GroupLimits = append(sub.GroupLimits, groupLimit)
		}
		if sub.PlanID != nil {
			plan := planDetailsByID[*sub.PlanID]
			sub.PlanLimits = plan.limits
			sub.PlanName = plan.name
		}
	}
	return nil
}

func loadCoveredGroupsBySubscription(ctx context.Context, client *dbent.Client, subIDs []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(subIDs))
	if len(subIDs) == 0 {
		return out, nil
	}
	rows, err := client.QueryContext(ctx,
		`SELECT subscription_id, group_id FROM user_subscription_groups
		 WHERE subscription_id = ANY($1) ORDER BY subscription_id, sort_order, group_id`,
		pq.Array(subIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var subID, groupID int64
		if err := rows.Scan(&subID, &groupID); err != nil {
			return nil, err
		}
		out[subID] = append(out[subID], groupID)
	}
	return out, rows.Err()
}

func loadGroupLimits(ctx context.Context, client *dbent.Client, groupIDs []int64) (map[int64]service.SubscriptionGroupLimit, error) {
	out := make(map[int64]service.SubscriptionGroupLimit, len(groupIDs))
	if len(groupIDs) == 0 {
		return out, nil
	}
	groups, err := client.Group.Query().
		Where(group.IDIn(groupIDs...)).
		Select(
			group.FieldID,
			group.FieldName,
			group.FieldDailyLimitUsd,
			group.FieldWeeklyLimitUsd,
			group.FieldMonthlyLimitUsd,
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range groups {
		out[item.ID] = service.SubscriptionGroupLimit{
			GroupID:         item.ID,
			Name:            item.Name,
			DailyLimitUSD:   item.DailyLimitUsd,
			WeeklyLimitUSD:  item.WeeklyLimitUsd,
			MonthlyLimitUSD: item.MonthlyLimitUsd,
		}
	}
	return out, nil
}

func loadGroupUsages(ctx context.Context, client *dbent.Client, subIDs []int64) (map[int64]map[int64]service.SubscriptionGroupUsage, error) {
	out := make(map[int64]map[int64]service.SubscriptionGroupUsage, len(subIDs))
	if len(subIDs) == 0 {
		return out, nil
	}
	rows, err := client.QueryContext(ctx, `
		SELECT subscription_id, group_id,
		       daily_window_start, weekly_window_start, monthly_window_start,
		       daily_usage_usd, weekly_usage_usd, monthly_usage_usd
		FROM user_subscription_group_usages
		WHERE subscription_id = ANY($1)`, pq.Array(subIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			subscriptionID, groupID               int64
			dailyStart, weeklyStart, monthlyStart *time.Time
			dailyUsage, weeklyUsage, monthlyUsage float64
		)
		if err := rows.Scan(&subscriptionID, &groupID, &dailyStart, &weeklyStart, &monthlyStart, &dailyUsage, &weeklyUsage, &monthlyUsage); err != nil {
			return nil, err
		}
		if out[subscriptionID] == nil {
			out[subscriptionID] = make(map[int64]service.SubscriptionGroupUsage)
		}
		out[subscriptionID][groupID] = service.SubscriptionGroupUsage{
			GroupID:            groupID,
			DailyWindowStart:   dailyStart,
			WeeklyWindowStart:  weeklyStart,
			MonthlyWindowStart: monthlyStart,
			DailyUsageUSD:      dailyUsage,
			WeeklyUsageUSD:     weeklyUsage,
			MonthlyUsageUSD:    monthlyUsage,
		}
	}
	return out, rows.Err()
}

type subscriptionPlanDetails struct {
	limits service.SubscriptionLimits
	name   string
}

func loadPlanDetails(ctx context.Context, client *dbent.Client, planIDs []int64) (map[int64]subscriptionPlanDetails, error) {
	out := make(map[int64]subscriptionPlanDetails, len(planIDs))
	if len(planIDs) == 0 {
		return out, nil
	}
	plans, err := client.SubscriptionPlan.Query().
		Where(subscriptionplan.IDIn(planIDs...)).
		Select(
			subscriptionplan.FieldID,
			subscriptionplan.FieldName,
			subscriptionplan.FieldDailyLimitUsd,
			subscriptionplan.FieldWeeklyLimitUsd,
			subscriptionplan.FieldMonthlyLimitUsd,
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		out[plan.ID] = subscriptionPlanDetails{
			name: plan.Name,
			limits: service.SubscriptionLimits{
				DailyLimitUSD:   plan.DailyLimitUsd,
				WeeklyLimitUSD:  plan.WeeklyLimitUsd,
				MonthlyLimitUSD: plan.MonthlyLimitUsd,
			},
		}
	}
	return out, nil
}

// entitiesToServiceHydrated 转换并批量补齐额度池信息（覆盖分组 + 套餐限额）。
// 列表路径也需要补齐：订阅列表要按共享额度展示剩余量，漏掉会显示成分组限额。
func (r *userSubscriptionRepository) entitiesToServiceHydrated(ctx context.Context, models []*dbent.UserSubscription) ([]service.UserSubscription, error) {
	out := userSubscriptionEntitiesToService(models)
	refs := make([]*service.UserSubscription, 0, len(out))
	for i := range out {
		refs = append(refs, &out[i])
	}
	if err := r.hydrateSubscriptions(ctx, refs); err != nil {
		return nil, err
	}
	return out, nil
}

func userSubscriptionEntitiesToService(models []*dbent.UserSubscription) []service.UserSubscription {
	out := make([]service.UserSubscription, 0, len(models))
	for i := range models {
		if s := userSubscriptionEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

func applyUserSubscriptionEntityToService(dst *service.UserSubscription, src *dbent.UserSubscription) {
	if dst == nil || src == nil {
		return
	}
	dst.ID = src.ID
	dst.CreatedAt = src.CreatedAt
	dst.UpdatedAt = src.UpdatedAt
}
