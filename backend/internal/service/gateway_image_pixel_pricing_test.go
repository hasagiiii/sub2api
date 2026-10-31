//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateRecordUsageCost_ImageCountBillingChannelPixelPricing(t *testing.T) {
	groupID := int64(78)
	model := "pixel-image-model"
	firstPrice, lastPrice := 0.12, 0.24
	maxPixels := int64(2_000_000)
	resolver := newOpenAIImageChannelPricingResolverForTest(t, groupID, model, firstPrice)
	pricing := ChannelModelPricing{
		Models: []string{model}, BillingMode: BillingModeImage,
		Intervals: []PricingInterval{
			{TierLabel: "P1", MaxPixels: &maxPixels, PerRequestPrice: &firstPrice},
			{TierLabel: "P2", PerRequestPrice: &lastPrice},
		},
	}
	cache := resolver.channelService.cache.Load().(*channelCache)
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = &pricing
	group := &Group{
		ID: groupID, Platform: PlatformOpenAI, Hydrated: true,
		AllowImageGeneration: true, ImageCountBilling: true,
	}
	apiKey := &APIKey{GroupID: &groupID, Group: group}
	svc := &GatewayService{billingService: resolver.billingService, resolver: resolver}

	// Final settlement must use actual pixels even with per-image billing enabled.
	// Normalizing to a named tier would reserve the highest price instead.
	cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
		Model: model, ImageCount: 2, ImageSize: "1024x1024", ImageEndpoint: true,
	}, apiKey, model, 3, 1.5, time.Time{})

	require.NotNil(t, cost)
	require.InDelta(t, 2*firstPrice, cost.TotalCost, 1e-12)
	require.InDelta(t, 2*firstPrice*1.5, cost.ActualCost, 1e-12,
		"image settlement must use the image multiplier")
}

func TestGatewayImageCostPreservesPixelDimensions(t *testing.T) {
	for _, source := range []string{PricingSourceChannel, PricingSourceGroup} {
		t.Run(source, func(t *testing.T) {
			groupID := int64(77)
			model := "pixel-image-model"
			firstPrice, lastPrice := 0.12, 0.24
			maxPixels := int64(2_000_000)
			resolver := newOpenAIImageChannelPricingResolverForTest(t, groupID, model, firstPrice)
			pricing := ChannelModelPricing{
				Models: []string{model}, BillingMode: BillingModeImage,
				Intervals: []PricingInterval{
					{TierLabel: "P1", MaxPixels: &maxPixels, PerRequestPrice: &firstPrice},
					{TierLabel: "P2", PerRequestPrice: &lastPrice},
				},
			}
			cache := resolver.channelService.cache.Load().(*channelCache)
			cache.pricingByGroupModel[channelModelKey{groupID: groupID, model: model}] = &pricing
			group := &Group{ID: groupID, Platform: PlatformOpenAI, Hydrated: true}
			if source == PricingSourceGroup {
				group.ModelPricing = []ChannelModelPricing{pricing}
			}
			svc := &GatewayService{billingService: resolver.billingService, resolver: resolver}
			cost := svc.calculateImageCost(context.Background(), &ForwardResult{ImageCount: 2, ImageSize: "1024x1024"}, &APIKey{GroupID: &groupID, Group: group}, model, 1)
			require.InDelta(t, 2*firstPrice, cost.TotalCost, 1e-12)
		})
	}
}
