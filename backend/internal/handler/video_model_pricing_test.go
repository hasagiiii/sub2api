//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPlaygroundPricingIncludesGroupImageAndRequestPrices(t *testing.T) {
	for _, mode := range []service.BillingMode{service.BillingModeImage, service.BillingModePerRequest} {
		t.Run(string(mode), func(t *testing.T) {
			price := 0.25
			h := &VideoModelHandler{pricingResolver: service.NewModelPricingResolver(nil, &service.BillingService{})}
			groups := []service.Group{{ID: 9, ModelPricing: []service.ChannelModelPricing{{Models: []string{"openai/gpt-image-2"}, BillingMode: mode, PerRequestPrice: &price}}}}
			rows := h.resolveVideoPricing(context.Background(), "openai/gpt-image-2", groups)
			require.Len(t, rows, 1)
			require.Equal(t, mode, rows[0].BillingMode)
			require.NotNil(t, rows[0].UnitPrice)
			require.Equal(t, price, *rows[0].UnitPrice)
			require.Zero(t, rows[0].PricePerSecond)
			require.True(t, rows[0].Enabled)
		})
	}
}

func TestPlaygroundImagePricingPreservesQualityAndDisabledTiers(t *testing.T) {
	low, high, disabled := 0.1, 0.3, 0.0
	h := &VideoModelHandler{pricingResolver: service.NewModelPricingResolver(nil, &service.BillingService{})}
	groups := []service.Group{{ID: 9, ModelPricing: []service.ChannelModelPricing{{Models: []string{"openai/gpt-image-2"}, BillingMode: service.BillingModeImage, Intervals: []service.PricingInterval{
		{TierLabel: "1K", Quality: "low", PerRequestPrice: &low},
		{TierLabel: "1K", Quality: "high", PerRequestPrice: &high},
		{TierLabel: "2K", Quality: "high", PerRequestPrice: &disabled},
	}}}}}
	rows := h.resolveVideoPricing(context.Background(), "openai/gpt-image-2", groups)
	require.Len(t, rows, 3)
	require.Equal(t, "low", rows[0].Quality)
	require.Equal(t, "high", rows[1].Quality)
	require.Equal(t, high, *rows[1].UnitPrice)
	require.False(t, rows[2].Enabled)
}

func TestPlaygroundImagePricingMatchesCanonicalModelFallback(t *testing.T) {
	canonical, public := 0.2, 0.4
	h := &VideoModelHandler{pricingResolver: service.NewModelPricingResolver(nil, &service.BillingService{})}
	groups := []service.Group{{ID: 9, ModelPricing: []service.ChannelModelPricing{
		{Models: []string{"gpt-image-2"}, BillingMode: service.BillingModeImage, PerRequestPrice: &canonical},
		{Models: []string{"openai/gpt-image-2"}, BillingMode: service.BillingModeImage, PerRequestPrice: &public},
	}}}
	rows := h.resolveVideoPricing(context.Background(), "openai/gpt-image-2", groups)
	require.Len(t, rows, 1)
	require.Equal(t, public, *rows[0].UnitPrice, "the configured public model price takes precedence")
	rows = h.resolveVideoPricing(context.Background(), "openai/gpt-image-2/edit", groups)
	require.Len(t, rows, 1)
	require.Equal(t, canonical, *rows[0].UnitPrice)
	require.Empty(t, h.resolveVideoPricing(context.Background(), "openai/gpt-imge-2", groups), "misspelled models must not silently match another model's pricing")
}

func TestPlaygroundImagePricingFallsBackToGroupMatrixAndLegacyTiers(t *testing.T) {
	legacy := 0.12
	h := &VideoModelHandler{pricingResolver: service.NewModelPricingResolver(nil, &service.BillingService{})}
	groups := []service.Group{{ID: 9, ImagePrice1K: &legacy, ImagePricingMatrix: domain.ImagePricingMatrix{"2K": {"high": 0.35, "low": 0.15}}}}
	rows := h.resolveVideoPricing(context.Background(), "openai/gpt-image-2", groups)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Equal(t, service.BillingModeImage, row.BillingMode)
		require.NotNil(t, row.UnitPrice)
	}
	require.Equal(t, "1K", rows[2].Resolution)
	require.Equal(t, legacy, *rows[2].UnitPrice)
	require.Empty(t, h.resolveVideoPricing(context.Background(), "bytedance/seedance-2.5/text-to-video", groups))
}
