package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterClientInfoStoresIPAndUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var gotIP, gotUA string
	router.POST("/auth/register", RegisterClientInfo(), func(c *gin.Context) {
		gotIP, gotUA = service.RegisterClientInfoFromContext(c.Request.Context())
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/register", nil)
	req.RemoteAddr = "198.51.100.9:43210"
	req.Header.Set("User-Agent", "Mozilla/5.0 (test)")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "198.51.100.9", gotIP)
	require.Equal(t, "Mozilla/5.0 (test)", gotUA)
}
