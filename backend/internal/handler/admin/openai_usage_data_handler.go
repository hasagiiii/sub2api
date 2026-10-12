package admin

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type openAIUsageDataService interface {
	QueryUsageData(context.Context, int64) (json.RawMessage, error)
	QueryUsageAnalytics(context.Context, int64, string, string, string) (json.RawMessage, error)
}

// QueryUsageData exposes the upstream usage payload through the admin API.
// GET /api/v1/admin/openai/accounts/:id/usage-data
func (h *OpenAIOAuthHandler) QueryUsageData(c *gin.Context) {
	h.queryUsageData(c, false)
}

// QueryUsageAnalytics exposes daily plugin, skill, token, or workspace usage.
// GET /api/v1/admin/openai/accounts/:id/usage-data/:metric
func (h *OpenAIOAuthHandler) QueryUsageAnalytics(c *gin.Context) {
	h.queryUsageData(c, true)
}

func (h *OpenAIOAuthHandler) queryUsageData(c *gin.Context, analytics bool) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.usageDataService == nil {
		response.BadRequest(c, "OpenAI usage data service is not enabled")
		return
	}
	c.Header("Cache-Control", "no-store")
	var payload json.RawMessage
	if analytics {
		payload, err = h.usageDataService.QueryUsageAnalytics(c.Request.Context(), accountID, c.Param("metric"), c.Query("start_date"), c.Query("end_date"))
	} else {
		payload, err = h.usageDataService.QueryUsageData(c.Request.Context(), accountID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, payload)
}
