package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RegisterClientInfo records the caller's IP and User-Agent in the request
// context so self-registration (email and third-party OAuth) can persist them
// on the newly created user. It is mounted only on public auth routes; users
// created by administrators keep empty values.
func RegisterClientInfo() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request != nil {
			ctx := service.WithRegisterClientInfo(c.Request.Context(), ip.GetClientIP(c), c.GetHeader("User-Agent"))
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
