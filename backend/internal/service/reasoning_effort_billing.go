package service

import (
	"math"
	"strings"
)

func reasoningEffortBillingMultiplier(effort string, multipliers map[string]float64) float64 {
	if len(multipliers) == 0 {
		return 1
	}
	key := strings.ToLower(strings.TrimSpace(effort))
	key = strings.NewReplacer("-", "", "_", "", " ", "").Replace(key)
	if key == "extrahigh" {
		key = "xhigh"
	}
	v, ok := multipliers[key]
	if !ok || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 1
	}
	return v
}
