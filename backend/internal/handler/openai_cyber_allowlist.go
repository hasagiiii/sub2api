package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) cyberPolicyLogOnly(c *gin.Context, apiKey *service.APIKey) bool {
	return h != nil && c != nil && c.Request != nil && h.gatewayService.CyberPolicyLogOnly(c.Request.Context(), apiKey)
}

// Preserve the resolved identity, including the identity inherited by a WS turn.
func (h *OpenAIGatewayHandler) findBlockedCyberIdentityForAPIKey(c *gin.Context, apiKey *service.APIKey, identity service.CyberSessionIdentityResolution) string {
	if apiKey == nil || h.cyberPolicyLogOnly(c, apiKey) {
		return ``
	}
	return h.gatewayService.FindCyberSessionBlockedForIdentity(c.Request.Context(), identity)
}
