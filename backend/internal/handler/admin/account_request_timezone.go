package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetOpenAIRequestTimezones returns the allowed OpenAI request timezones.
// GET /api/v1/admin/accounts/openai-request-timezones
func (h *AccountHandler) GetOpenAIRequestTimezones(c *gin.Context) {
	response.Success(c, gin.H{
		"default":   service.DefaultOpenAIRequestTimezone,
		"timezones": service.OpenAIRequestTimezoneOptions(),
	})
}
