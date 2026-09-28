package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

var ErrImagePricingModelUnsupported = errors.New("group does not support model")

type ImagePricingEstimate struct {
	Endpoint       string          `json:"endpoint"`
	BillingMode    string          `json:"billing_mode"`
	PricingSource  string          `json:"pricing_source"`
	Tier           string          `json:"tier"`
	Resolution     ImageDimensions `json:"resolution"`
	ImageCount     int             `json:"image_count"`
	UnitPrice      float64         `json:"unit_price"`
	TotalCost      float64         `json:"total_cost"`
	RateMultiplier float64         `json:"rate_multiplier"`
	EstimatedPrice float64         `json:"estimated_price"`
}

func (s *GatewayService) EstimateImagePricing(
	ctx context.Context,
	apiKey *APIKey,
	endpoint string,
	dimensions ImageDimensions,
	quality string,
	imageCount int,
) (*ImagePricingEstimate, error) {
	if s == nil || s.billingService == nil {
		return nil, fmt.Errorf("billing service is unavailable")
	}
	if apiKey == nil || apiKey.Group == nil {
		return nil, fmt.Errorf("API key group is required")
	}
	endpoint = strings.Trim(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("model endpoint is required")
	}
	if err := s.validateGroupSupportsPricingModel(ctx, apiKey, endpoint); err != nil {
		return nil, err
	}
	if imageCount <= 0 {
		imageCount = 1
	}
	multiplier := resolveImageRateMultiplier(apiKey, apiKey.Group.RateMultiplier)
	gid := apiKey.Group.ID

	if s.resolver != nil {
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: endpoint, GroupID: &gid, Group: apiKey.Group})
		if resolved != nil && (resolved.Mode == BillingModeImage || resolved.Mode == BillingModePerRequest) &&
			(len(resolved.RequestTiers) > 0 || resolved.DefaultPerRequestPrice > 0) {
			tier := ""
			if resolved.Mode == BillingModeImage && len(resolved.RequestTiers) > 0 {
				_, matchedTier, err := s.resolver.GetImageTierPrice(resolved, dimensions, quality)
				if err != nil {
					return nil, err
				}
				tier = matchedTier
			} else {
				matched, err := MatchImagePricingTier(dimensions, apiKey.Group.ImagePricingTiers())
				if err != nil {
					return nil, err
				}
				tier = matched.Label
			}
			cost, err := s.billingService.CalculateCostUnified(CostInput{
				Ctx: ctx, Model: endpoint, GroupID: &gid, Group: apiKey.Group,
				RequestCount: imageCount, SizeTier: dimensions.String(), Quality: quality,
				RateMultiplier: multiplier, Resolver: s.resolver, Resolved: resolved,
			})
			if err != nil {
				return nil, err
			}
			estimate := newImagePricingEstimate(endpoint, dimensions, imageCount, multiplier, tier, resolved.Source, cost)
			s.logImagePricingEstimate(apiKey, endpoint, dimensions, quality, estimate, cost, resolved, "resolved_pricing", nil)
			return estimate, nil
		}
	}

	matched, err := MatchImagePricingTier(dimensions, apiKey.Group.ImagePricingTiers())
	if err != nil {
		return nil, err
	}
	groupConfig := apiKey.Group.BuildImagePriceConfig(dimensions.Width, dimensions.Height, quality)
	cost, err := s.billingService.CalculateImageCostWithQualityValidated(
		endpoint, dimensions.String(), quality, imageCount, groupConfig, multiplier,
	)
	if err != nil {
		return nil, err
	}
	estimate := newImagePricingEstimate(endpoint, dimensions, imageCount, multiplier, matched.Label, PricingSourceGroup, cost)
	s.logImagePricingEstimate(apiKey, endpoint, dimensions, quality, estimate, cost, nil, "group_fallback", apiKey.Group.ImagePricingTiers())
	return estimate, nil
}

// logImagePricingEstimate records every input that contributes to the public
// estimate response. It intentionally excludes request bodies and image URLs.
func (s *GatewayService) logImagePricingEstimate(
	apiKey *APIKey,
	endpoint string,
	dimensions ImageDimensions,
	quality string,
	estimate *ImagePricingEstimate,
	cost *CostBreakdown,
	resolved *ResolvedPricing,
	pricingPath string,
	groupTiers []ImagePricingTier,
) {
	if estimate == nil {
		return
	}
	fields := []zap.Field{
		zap.Int64("api_key_id", apiKey.ID),
		zap.Int64("group_id", apiKey.Group.ID),
		zap.String("group_name", apiKey.Group.Name),
		zap.String("endpoint", endpoint),
		zap.String("pricing_path", pricingPath),
		zap.String("pricing_source", estimate.PricingSource),
		zap.String("billing_mode", estimate.BillingMode),
		zap.String("matched_tier", estimate.Tier),
		zap.String("quality", quality),
		zap.Int("width", dimensions.Width),
		zap.Int("height", dimensions.Height),
		zap.Int64("pixels", dimensions.Pixels()),
		zap.Int("image_count", estimate.ImageCount),
		zap.Float64("rate_multiplier", estimate.RateMultiplier),
		zap.Float64("cost_total", estimate.TotalCost),
		zap.Float64("cost_actual", estimate.EstimatedPrice),
		zap.Float64("returned_unit_price", estimate.UnitPrice),
		zap.Float64("returned_total_price", estimate.EstimatedPrice),
	}
	if cost != nil {
		fields = append(fields, zap.String("cost_billing_mode", cost.BillingMode))
	}
	if resolved != nil {
		fields = append(fields,
			zap.String("resolved_source", resolved.Source),
			zap.String("resolved_mode", string(resolved.Mode)),
			zap.Int("resolved_request_tier_count", len(resolved.RequestTiers)),
			zap.Float64("resolved_default_per_request_price", resolved.DefaultPerRequestPrice),
			zap.Any("resolved_request_tiers", imagePricingEstimateIntervalDiagnostics(s.resolver, resolved, quality)),
		)
	}
	if len(groupTiers) > 0 {
		fields = append(fields, zap.Any("group_image_tiers", imagePricingEstimateGroupTierDiagnostics(groupTiers)))
	}
	logger.L().Info("image.pricing_estimate_resolved", fields...)
}

func imagePricingEstimateIntervalDiagnostics(
	resolver *ModelPricingResolver,
	resolved *ResolvedPricing,
	quality string,
) []map[string]any {
	if resolved == nil {
		return nil
	}
	rows := make([]map[string]any, 0, len(resolved.RequestTiers))
	for _, interval := range resolved.RequestTiers {
		row := map[string]any{
			"tier_label":        interval.TierLabel,
			"resolution":        interval.Resolution,
			"quality":           interval.Quality,
			"max_pixels":        interval.MaxPixels,
			"per_request_price": interval.PerRequestPrice,
		}
		if resolver != nil {
			row["effective_price"] = resolver.GetRequestTierPriceWithQuality(resolved, interval.TierLabel, quality)
		}
		rows = append(rows, row)
	}
	return rows
}

func imagePricingEstimateGroupTierDiagnostics(tiers []ImagePricingTier) []map[string]any {
	rows := make([]map[string]any, 0, len(tiers))
	for _, tier := range tiers {
		rows = append(rows, map[string]any{
			"label":      tier.Label,
			"resolution": tier.Resolution,
			"price":      tier.Price,
		})
	}
	return rows
}

func (s *GatewayService) validateGroupSupportsPricingModel(ctx context.Context, apiKey *APIKey, endpoint string) error {
	if s == nil || s.accountRepo == nil {
		return fmt.Errorf("group model validation is unavailable")
	}
	groupID := apiKey.GroupID
	if groupID == nil && apiKey.Group != nil {
		id := apiKey.Group.ID
		groupID = &id
	}
	requestedModel := strings.ToLower(strings.Trim(strings.TrimSpace(endpoint), "/"))
	requestedPath := normalizeFalModelPath(requestedModel)
	for _, supportedModel := range s.GetAvailableModels(ctx, groupID, "") {
		pattern := strings.ToLower(strings.Trim(strings.TrimSpace(supportedModel), "/"))
		if pattern == requestedModel || matchWildcard(pattern, requestedModel) {
			return nil
		}
		// Account/channel mappings may expose the same FAL endpoint with or
		// without the transport prefix. Treat both forms as the same model.
		patternPath := normalizeFalModelPath(pattern)
		if patternPath == requestedPath || matchWildcard(patternPath, requestedPath) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrImagePricingModelUnsupported, endpoint)
}

func newImagePricingEstimate(endpoint string, dimensions ImageDimensions, imageCount int, multiplier float64, tier, source string, cost *CostBreakdown) *ImagePricingEstimate {
	estimate := &ImagePricingEstimate{
		Endpoint: endpoint, BillingMode: string(BillingModeImage), PricingSource: source,
		Tier: tier, Resolution: dimensions, ImageCount: imageCount, RateMultiplier: multiplier,
	}
	if cost != nil {
		estimate.TotalCost = cost.TotalCost
		estimate.EstimatedPrice = cost.ActualCost
		if imageCount > 0 {
			estimate.UnitPrice = cost.TotalCost / float64(imageCount)
		}
		if cost.BillingMode != "" {
			estimate.BillingMode = cost.BillingMode
		}
	}
	return estimate
}
