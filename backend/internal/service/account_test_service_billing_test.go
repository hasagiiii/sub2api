package service

import (
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountTestServiceEnrichUsageEventCostUsesAccountMultiplier(t *testing.T) {
	multiplier := 1.5
	account := &Account{RateMultiplier: &multiplier}
	service := &AccountTestService{billingService: NewBillingService(&config.Config{}, nil)}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Set(accountTestBillingContextKey, accountTestBillingContext{
		account: account,
		model:   "gpt-6-astra",
	})
	event := TestEvent{
		Type:         "usage",
		InputTokens:  100,
		OutputTokens: 20,
		TotalTokens:  120,
	}

	service.enrichUsageEventCost(context, &event)

	require.InDelta(t, 0.003, event.CostUSD, 1e-12)
}
