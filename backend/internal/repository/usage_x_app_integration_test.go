//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAsyncTasksPersistSubmittingAppThroughTerminalUsage(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	user := mustCreateUser(t, client, &service.User{Email: "x-app-async@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID})
	account := mustCreateAccount(t, client, &service.Account{Name: "x-app-test"})
	submitCtx := context.WithValue(context.Background(), ctxkey.XApp, "opentk")
	// The polling request has a different app; final usage must retain the submitter.
	pollCtx := context.WithValue(context.Background(), ctxkey.XApp, "poller")

	t.Run("image", func(t *testing.T) {
		repo := &asyncMediaTaskRepository{sql: tx}
		task := &service.AsyncMediaTask{InternalRequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID, AccountID: &account.ID, RequestedModel: "image/model"}
		require.NoError(t, repo.Create(submitCtx, task))
		stored, err := repo.GetByID(pollCtx, task.ID)
		require.NoError(t, err)
		require.Equal(t, "opentk", *stored.XApp)
		inserted, err := repo.InsertTerminalUsageLog(pollCtx, &service.TerminalUsageLogInput{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: task.InternalRequestID,
			Model: task.RequestedModel, TaskID: task.ID, XApp: *stored.XApp, BillingStatus: service.BillingStatusCharged,
		})
		require.NoError(t, err)
		require.True(t, inserted)
		var usageID int64
		require.NoError(t, scanSingleRow(pollCtx, tx, "SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", []any{task.InternalRequestID, key.ID}, &usageID))
		usage, err := newUsageLogRepositoryWithSQL(client, tx).GetByID(pollCtx, usageID)
		require.NoError(t, err)
		require.Equal(t, "opentk", *usage.XApp)
	})
	t.Run("video", func(t *testing.T) {
		repo := &asyncVideoTaskRepository{sql: tx}
		task := &service.AsyncVideoTask{InternalRequestID: uuid.NewString(), UserID: user.ID, APIKeyID: key.ID, AccountID: &account.ID, RequestedModel: "video/model"}
		require.NoError(t, repo.Create(submitCtx, task))
		stored, err := repo.GetByID(pollCtx, task.ID)
		require.NoError(t, err)
		require.Equal(t, "opentk", *stored.XApp)
		inserted, err := repo.InsertTerminalUsageLog(pollCtx, &service.VideoTerminalUsageLogInput{
			UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: task.InternalRequestID,
			Model: task.RequestedModel, TaskID: task.ID, XApp: *stored.XApp, BillingStatus: service.BillingStatusCharged,
		})
		require.NoError(t, err)
		require.True(t, inserted)
		var usageID int64
		require.NoError(t, scanSingleRow(pollCtx, tx, "SELECT id FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", []any{task.InternalRequestID, key.ID}, &usageID))
		usage, err := newUsageLogRepositoryWithSQL(client, tx).GetByID(pollCtx, usageID)
		require.NoError(t, err)
		require.Equal(t, "opentk", *usage.XApp)
	})
}
