//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBytedanceOnlyEntersModelImagePool(t *testing.T) {
	groupID := int64(8888)
	account := Account{ID: 1, Platform: PlatformBytedance, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	svc := &GatewayService{accountRepo: newImageMixedRepo([]Account{account}), cache: &mockGatewayCacheForPlatform{}, cfg: testConfig(), resolver: newFalUpstreamPricingResolver(t, groupID, domain.SeedreamModel, 0.1)}
	ctx := withTestGroup(context.Background(), groupID)
	selected, err := svc.SelectAsyncImageAccountInGroup(ctx, &groupID, "", domain.SeedreamModel, nil, "")
	require.NoError(t, err)
	require.Equal(t, PlatformBytedance, selected.Platform)
	_, err = svc.SelectImageAccountMixed(ctx, &groupID, "", domain.SeedreamModel, nil, OpenAIImagesCapabilityBasic, "", "")
	require.Error(t, err)
	require.False(t, isConcreteRequestPlatform(PlatformBytedance))
}
