package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.BytedanceExecutionRepository = (*asyncMediaTaskRepository)(nil)

func (r *asyncMediaTaskRepository) CreateBytedance(ctx context.Context, task *service.AsyncMediaTask, execution *service.BytedanceExecution) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if execution.BillingType == service.BillingTypeBalance {
		if err = adjustBytedanceBalance(ctx, tx, task, task.HeldCost); err != nil {
			return err
		}
	}
	local := &asyncMediaTaskRepository{sql: tx}
	if err = local.Create(ctx, task); err != nil {
		return err
	}
	payload, err := json.Marshal(execution.RequestPayload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bytedance_image_executions (task_id,request_payload,billing_type,unit_price) VALUES ($1,$2,$3,$4)`, task.ID, payload, execution.BillingType, execution.UnitPrice)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *asyncMediaTaskRepository) GetBytedance(ctx context.Context, id int64) (*service.BytedanceExecution, error) {
	e := &service.BytedanceExecution{TaskID: id}
	var request, result []byte
	var count sql.NullInt64
	var reason sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT request_payload,result_payload,state,billing_type,unit_price,billable_images,billing_error FROM bytedance_image_executions WHERE task_id=$1`, id).Scan(&request, &result, &e.State, &e.BillingType, &e.UnitPrice, &count, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(request, &e.RequestPayload); err != nil {
		return nil, err
	}
	if len(result) > 0 {
		if err = json.Unmarshal(result, &e.ResultPayload); err != nil {
			return nil, err
		}
	}
	if count.Valid {
		n := int(count.Int64)
		e.BillableImages = &n
	}
	e.BillingError = reason.String
	return e, nil
}

func (r *asyncMediaTaskRepository) ClaimBytedance(ctx context.Context, id int64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var accountID int64
	var capacity int
	err = tx.QueryRowContext(ctx, `SELECT a.id,a.concurrency FROM accounts a JOIN async_media_tasks t ON t.account_id=a.id WHERE t.id=$1 FOR UPDATE OF a`, id).Scan(&accountID, &capacity)
	if err != nil {
		return false, err
	}
	if capacity < 1 {
		capacity = 1
	}
	var running int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bytedance_image_executions e JOIN async_media_tasks t ON t.id=e.task_id WHERE t.account_id=$1 AND e.state='running'`, accountID).Scan(&running)
	if err != nil {
		return false, err
	}
	if running >= capacity {
		return false, nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE bytedance_image_executions SET state='running',started_at=NOW(),updated_at=NOW() WHERE task_id=$1 AND state='pending'`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if _, err = tx.ExecContext(ctx, `UPDATE async_media_tasks SET status='running',updated_at=NOW() WHERE id=$1 AND status='pending'`, id); err != nil {
			return false, err
		}
	}
	return n == 1, tx.Commit()
}

func (r *asyncMediaTaskRepository) SaveBytedanceResult(ctx context.Context, id int64, result map[string]any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE bytedance_image_executions SET result_payload=$2,state='result_ready',updated_at=NOW() WHERE task_id=$1 AND state='running'`, id, raw)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("image execution is no longer running")
	}
	return err
}

func (r *asyncMediaTaskRepository) SettleBytedance(ctx context.Context, task *service.AsyncMediaTask, count int, finalCost float64, reason string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	var billingType int8
	var unitPrice float64
	err = tx.QueryRowContext(ctx, `SELECT state,billing_type,unit_price FROM bytedance_image_executions WHERE task_id=$1 FOR UPDATE`, task.ID).Scan(&state, &billingType, &unitPrice)
	if err != nil {
		return false, err
	}
	if state != "result_ready" && state != "billing_failed" {
		return false, nil
	}
	if reason == "" && billingType == service.BillingTypeBalance {
		if err = adjustBytedanceBalance(ctx, tx, task, finalCost-task.HeldCost); err != nil {
			return false, err
		}
	}
	local := &asyncMediaTaskRepository{sql: tx}
	// A manual settlement updates a previously completed, unpaid result.
	if state == "billing_failed" {
		_, err = tx.ExecContext(ctx, `UPDATE async_media_tasks SET final_cost=$2,error_reason=$3,updated_at=NOW() WHERE id=$1`, task.ID, finalCost, nullIfEmpty(reason))
	} else {
		_, err = local.MarkSucceeded(ctx, task.ID, task.ImageURLs, task.CosURLs, task.ImageMetadata, task.ResultPayload, finalCost)
	}
	if err != nil {
		return false, err
	}
	next := "settled"
	if reason != "" {
		next = "billing_failed"
		_, err = tx.ExecContext(ctx, `UPDATE async_media_tasks SET error_reason=$2 WHERE id=$1`, task.ID, reason)
		if err != nil {
			return false, err
		}
	}
	var billable any
	if count >= 0 {
		billable = count
	}
	_, err = tx.ExecContext(ctx, `UPDATE bytedance_image_executions SET state=$2,billable_images=$3,billing_error=$4,updated_at=NOW() WHERE task_id=$1`, task.ID, next, billable, nullIfEmpty(reason))
	if err != nil {
		return false, err
	}
	status := service.BillingStatusCharged
	if reason != "" {
		status = service.BillingStatusFailed
	}
	if _, err = local.InsertTerminalUsageLog(ctx, service.BytedanceTerminalUsageInput(task, count, finalCost, unitPrice, billingType, status)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *asyncMediaTaskRepository) RefundBytedance(ctx context.Context, id int64, reason string, cancel bool) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	var billingType int8
	err = tx.QueryRowContext(ctx, `SELECT state,billing_type FROM bytedance_image_executions WHERE task_id=$1 FOR UPDATE`, id).Scan(&state, &billingType)
	if err != nil {
		return false, err
	}
	if cancel && state != "pending" {
		return false, service.ErrBytedanceAlreadyRunning
	}
	if state != "pending" && state != "running" && state != "result_ready" {
		return false, nil
	}
	local := &asyncMediaTaskRepository{sql: tx}
	task, err := local.GetByID(ctx, id)
	if err != nil {
		return false, err
	}
	if task == nil {
		return false, sql.ErrNoRows
	}
	if billingType == service.BillingTypeBalance {
		if err = adjustBytedanceBalance(ctx, tx, task, -task.HeldCost); err != nil {
			return false, err
		}
	}
	if _, err = local.MarkRefunded(ctx, id, service.AsyncMediaStatusRefunded, reason); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE bytedance_image_executions SET state='refunded',billing_error=$2,updated_at=NOW() WHERE task_id=$1`, id, reason)
	if err != nil {
		return false, err
	}
	if _, err = local.InsertTerminalUsageLog(ctx, service.BytedanceTerminalUsageInput(task, 0, 0, 0, billingType, service.BillingStatusRefunded)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func adjustBytedanceBalance(ctx context.Context, tx *sql.Tx, task *service.AsyncMediaTask, delta float64) error {
	if delta == 0 {
		return nil
	}
	var res sql.Result
	var err error
	if task.BalanceSource != nil && *task.BalanceSource == service.BalanceSourceCompany {
		if task.OrganizationID == nil {
			return service.ErrCompanyNotFound
		}
		res, err = tx.ExecContext(ctx, `UPDATE organizations SET balance=balance-$1,updated_at=NOW() WHERE id=$2 AND ($1::numeric<=0 OR balance >= $1)`, delta, *task.OrganizationID)
	} else {
		payer := task.UserID
		if task.PayerUserID != nil {
			payer = *task.PayerUserID
		}
		res, err = tx.ExecContext(ctx, `UPDATE users SET balance=balance-$1,updated_at=NOW() WHERE id=$2 AND deleted_at IS NULL AND ($1::numeric<=0 OR balance >= $1)`, delta, payer)
	}
	if err != nil {
		return fmt.Errorf("adjust image balance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrInsufficientBalance
	}
	return nil
}
