package handler

import (
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func playgroundPrice(mode service.BillingMode, label, quality string, price float64) videoModelPricingItem {
	item := videoModelPricingItem{Resolution: label, Quality: quality, Currency: "USD", Enabled: price > 0}
	if mode == service.BillingModeVideo {
		item.PricePerSecond = price
	} else {
		item.BillingMode = mode
		item.UnitPrice = &price
	}
	return item
}

func playgroundResolvedPrices(resolved *service.ResolvedPricing) []videoModelPricingItem {
	if resolved == nil {
		return nil
	}
	switch resolved.Mode {
	case service.BillingModeVideo, service.BillingModeImage, service.BillingModePerRequest:
	default:
		return nil
	}
	items := make([]videoModelPricingItem, 0, len(resolved.RequestTiers)+1)
	for _, tier := range resolved.RequestTiers {
		if tier.PerRequestPrice == nil {
			continue
		}
		label := strings.TrimSpace(tier.TierLabel)
		if label == "" {
			label = strings.TrimSpace(tier.Resolution)
		}
		if label == "" {
			continue
		}
		items = append(items, playgroundPrice(resolved.Mode, label, tier.Quality, *tier.PerRequestPrice))
	}
	if resolved.DefaultPerRequestPrice > 0 {
		items = append(items, playgroundPrice(resolved.Mode, "default", "", resolved.DefaultPerRequestPrice))
	}
	return items
}

// Legacy group-wide image prices remain a fallback after model-specific prices,
// matching the asynchronous image task's billing priority.
func playgroundGroupImagePrices(group *service.Group) []videoModelPricingItem {
	if group == nil {
		return nil
	}
	var items []videoModelPricingItem
	tiers := make([]string, 0, len(group.ImagePricingMatrix))
	for tier := range group.ImagePricingMatrix {
		tiers = append(tiers, tier)
	}
	sort.Strings(tiers)
	for _, tier := range tiers {
		qualities := make([]string, 0, len(group.ImagePricingMatrix[tier]))
		for quality := range group.ImagePricingMatrix[tier] {
			qualities = append(qualities, quality)
		}
		sort.Strings(qualities)
		for _, quality := range qualities {
			items = append(items, playgroundPrice(service.BillingModeImage, tier, quality, group.ImagePricingMatrix[tier][quality]))
		}
	}
	for _, tier := range group.ImagePricingTiers() {
		if tier.Price != nil {
			items = append(items, playgroundPrice(service.BillingModeImage, tier.Label, "", *tier.Price))
		}
	}
	return items
}
