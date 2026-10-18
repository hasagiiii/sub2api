package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.InlineImageRepository = (*asyncMediaTaskRepository)(nil)

func (r *asyncMediaTaskRepository) CreateInlineImage(ctx context.Context, task *service.AsyncMediaTask, e *service.InlineImageExecution) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize simultaneous retries before reserving funds. The task ID remains globally unique.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, task.InternalRequestID); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM async_media_tasks WHERE internal_request_id=$1)`, task.InternalRequestID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return service.ErrInlineImageConflict
	}
	if e.BillingType == service.BillingTypeBalance {
		if err = adjustBytedanceBalance(ctx, tx, task, task.HeldCost); err != nil {
			return err
		}
	}
	local := &asyncMediaTaskRepository{sql: tx}
	if err = local.Create(ctx, task); err != nil {
		return err
	}
	raw, err := json.Marshal(e.RequestPayload)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inline_image_executions(task_id,request_payload,request_hash,billing_type) VALUES($1,$2,$3,$4)`, task.ID, raw, e.RequestHash, e.BillingType); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *asyncMediaTaskRepository) GetInlineImage(ctx context.Context, id int64) (*service.InlineImageExecution, error) {
	e := &service.InlineImageExecution{TaskID: id}
	var request, result []byte
	err := scanSingleRow(ctx, r.sql, `SELECT request_payload,request_hash,billing_type,state,result,COALESCE(failure_reason,''),failure_definitive FROM inline_image_executions WHERE task_id=$1`, []any{id}, &request, &e.RequestHash, &e.BillingType, &e.State, &result, &e.FailureReason, &e.FailureDefinitive)
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
		if err = json.Unmarshal(result, &e.Result); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (r *asyncMediaTaskRepository) ClaimInlineImage(ctx context.Context, id int64, token string) (bool, error) {
	res, err := r.sql.ExecContext(ctx, `WITH claimed AS (UPDATE inline_image_executions SET state=CASE WHEN state='pending' THEN 'generating' ELSE state END,lease_token=$2,lease_until=NOW()+INTERVAL '90 seconds',updated_at=NOW() WHERE task_id=$1 AND state IN ('pending','result_ready','stored','failure_ready') AND (lease_until IS NULL OR lease_until<NOW()) RETURNING task_id) UPDATE async_media_tasks SET status='running',updated_at=NOW() WHERE id IN (SELECT task_id FROM claimed)`, id, token)
	return inlineRowsAffected(res, err)
}
func (r *asyncMediaTaskRepository) RenewInlineImage(ctx context.Context, id int64, token string) (bool, error) {
	res, err := r.sql.ExecContext(ctx, `UPDATE inline_image_executions SET lease_until=NOW()+INTERVAL '90 seconds',updated_at=NOW() WHERE task_id=$1 AND lease_token=$2 AND state IN ('generating','result_ready','stored','failure_ready')`, id, token)
	return inlineRowsAffected(res, err)
}
func (r *asyncMediaTaskRepository) ReleaseInlineImage(ctx context.Context, id int64, token string) error {
	_, err := r.sql.ExecContext(ctx, `UPDATE inline_image_executions SET lease_until=NULL WHERE task_id=$1 AND lease_token=$2`, id, token)
	return err
}
func inlineRowsAffected(res sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r *asyncMediaTaskRepository) SaveInlineImageResult(ctx context.Context, id int64, token string, result *service.AsyncMediaInlineResult, stored bool) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	state := "result_ready"
	if stored {
		state = "stored"
	}
	ok, err := inlineRowsAffected(r.sql.ExecContext(ctx, `UPDATE inline_image_executions SET result=$3,state=$4,updated_at=NOW() WHERE task_id=$1 AND lease_token=$2 AND state IN ('generating','result_ready','stored')`, id, token, raw, state))
	if err == nil && !ok {
		return errors.New("image execution ownership lost")
	}
	return err
}
func (r *asyncMediaTaskRepository) SaveInlineImageFailure(ctx context.Context, id int64, token, reason string, definitive bool) error {
	ok, err := inlineRowsAffected(r.sql.ExecContext(ctx, `UPDATE inline_image_executions SET failure_reason=$3,failure_definitive=$4,state='failure_ready',updated_at=NOW() WHERE task_id=$1 AND lease_token=$2 AND state='generating'`, id, token, reason, definitive))
	if err == nil && !ok {
		return errors.New("image execution ownership lost")
	}
	return err
}

func (r *asyncMediaTaskRepository) FinishInlineImage(ctx context.Context, task *service.AsyncMediaTask, in *service.TerminalUsageLogInput) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	var billing int8
	if err = tx.QueryRowContext(ctx, `SELECT state,billing_type FROM inline_image_executions WHERE task_id=$1 FOR UPDATE`, task.ID).Scan(&state, &billing); err != nil {
		return false, err
	}
	if state != "stored" {
		return false, nil
	}
	if billing == service.BillingTypeBalance {
		if err = adjustBytedanceBalance(ctx, tx, task, in.ActualCost-task.HeldCost); err != nil {
			return false, err
		}
	}
	local := &asyncMediaTaskRepository{sql: tx}
	ok, err := local.MarkSucceeded(ctx, task.ID, task.ImageURLs, task.CosURLs, task.ImageMetadata, task.ResultPayload, in.ActualCost)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errors.New("image task is already terminal")
	}
	if _, err = local.InsertTerminalUsageLog(ctx, in); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE inline_image_executions SET state='settled',request_payload='{}'::jsonb,result=NULL,lease_until=NULL,updated_at=NOW() WHERE task_id=$1`, task.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Refund only known failures; a lost synchronous response must never trigger a second generation.
func (r *asyncMediaTaskRepository) FailInlineImage(ctx context.Context, id int64, reason string, definitive, cancel bool) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var state string
	var billing int8
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT state,billing_type,COALESCE(lease_until>NOW(),false) FROM inline_image_executions WHERE task_id=$1 FOR UPDATE`, id).Scan(&state, &billing, &active); err != nil {
		return false, err
	}
	if cancel && state != "pending" {
		return false, service.ErrInlineImageRunning
	}
	if state == "settled" || state == "failed" || state == "unknown" || state == "result_ready" || state == "stored" {
		return false, nil
	}
	if state == "generating" && active {
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
	status := service.BillingStatusFailed
	next := "unknown"
	code := "generation_outcome_unknown"
	cost := task.HeldCost
	if definitive {
		if billing == service.BillingTypeBalance {
			if err = adjustBytedanceBalance(ctx, tx, task, -task.HeldCost); err != nil {
				return false, err
			}
		}
		if _, err = local.MarkRefunded(ctx, id, service.AsyncMediaStatusRefunded, reason); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE async_media_tasks SET error_code='generation_failed' WHERE id=$1`, id); err != nil {
			return false, err
		}
		status = service.BillingStatusRefunded
		next = "failed"
		cost = 0
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE async_media_tasks SET status='failed',error_reason=$2,error_code=$3,finished_at=NOW(),updated_at=NOW() WHERE id=$1`, id, reason, code); err != nil {
			return false, err
		}
	}
	in := service.InlineImageTerminalUsage(task, billing, cost, cost, status)
	if _, err = local.InsertTerminalUsageLog(ctx, in); err != nil {
		return false, fmt.Errorf("persist image failure usage: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE inline_image_executions SET state=$2,failure_reason=$3,lease_until=NULL,updated_at=NOW() WHERE task_id=$1`, id, next, reason); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *asyncMediaTaskRepository) GetInlineImageState(ctx context.Context, id int64) (string, error) {
	var state string
	err := scanSingleRow(ctx, r.sql, `SELECT state FROM inline_image_executions WHERE task_id=$1`, []any{id}, &state)
	return state, err
}
