package routes

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminOrganizationConsoleMirrorsMemberConsoleRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &handler.Handlers{Organization: &handler.OrganizationHandler{}}
	router := gin.New()
	pass := func(c *gin.Context) { c.Next() }
	RegisterOrganizationRoutes(router.Group("/api/v1"), h, pass, pass, nil)
	registerOrganizationAdminRoutes(router.Group("/api/v1/admin"), h)

	member := map[string]bool{}
	admin := map[string]bool{}
	for _, route := range router.Routes() {
		key := route.Method + " "
		switch {
		case strings.HasPrefix(route.Path, "/api/v1/admin/organizations/:organization_id/console/"):
			admin[key+strings.TrimPrefix(route.Path, "/api/v1/admin/organizations/:organization_id/console")] = true
		case strings.HasPrefix(route.Path, "/api/v1/organization/"):
			member[key+strings.TrimPrefix(route.Path, "/api/v1/organization")] = true
		}
	}

	personal := map[string]bool{
		http.MethodPost + " /name-change-requests":     true,
		http.MethodPut + " /password":                  true,
		http.MethodPost + " /recovery-email/send-code": true,
		http.MethodPost + " /recovery-email/verify":    true,
		http.MethodPost + " /subscription-orders":      true,
	}
	for route := range member {
		if strings.Contains(route, " /applications") {
			continue // company upgrade applications belong to the applicant only
		}
		if personal[route] {
			require.False(t, admin[route], "personal endpoint must not be exposed to administrators: %s", route)
			continue
		}
		require.True(t, admin[route], "admin console is missing %s", route)
	}
	for route := range admin {
		require.True(t, member[route], "admin console exposes an endpoint the member console lacks: %s", route)
	}
	require.True(t, admin[http.MethodPost+" /company-balance"])
	require.True(t, admin[http.MethodPost+" /members"])
}
