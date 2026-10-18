//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type xAppVideoTaskRepo struct {
	AsyncVideoTaskRepository
	usage *VideoTerminalUsageLogInput
}

func (r *xAppVideoTaskRepo) InsertTerminalUsageLog(_ context.Context, in *VideoTerminalUsageLogInput) (bool, error) {
	r.usage = in
	return true, nil
}
func TestVideoTerminalUsagePreservesSubmittingApp(t *testing.T) {
	repo := &xAppVideoTaskRepo{}
	svc := &AsyncVideoService{taskRepo: repo}
	task := &AsyncVideoTask{ID: 1, XApp: amStrPtr("opentk")}
	svc.writeTerminalUsageLog(context.WithValue(context.Background(), ctxkey.XApp, "poller"), task, BillingTypeBalance, 1, BillingStatusCharged, nil, nil)
	require.Equal(t, "opentk", repo.usage.XApp)
}
