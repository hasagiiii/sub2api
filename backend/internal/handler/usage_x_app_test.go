//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageRecordTaskPreservesXAppAfterRequestEnds(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxkey.XApp, "opentk"))
	cancel()
	task, _ := wrapUsageRecordTaskContext(parent, func(ctx context.Context) {
		require.NoError(t, ctx.Err())
		require.Equal(t, "opentk", *service.RequestAppFromContext(ctx))
	})
	task(context.Background())
}
