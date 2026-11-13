package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// AdminConsoleActorContextKey records the system administrator acting on behalf
// of an enterprise owner. Present only on /admin/organizations/:id/console/*.
const AdminConsoleActorContextKey = "organization_admin_console_actor"

// AdminActAsOrganizationOwner lets a system administrator use the enterprise
// console for one organization. It resolves that organization's owner and
// rewrites the authenticated subject to the owner, so the regular console
// handlers and repository ACLs apply unchanged (owner scope only, never other
// organizations). The administrator stays the recorded audit actor.
//
// Scoped admin API keys are rejected: console operations move company funds and
// manage IAM members, which no scoped permission grants.
func (h *OrganizationHandler) AdminActAsOrganizationOwner(c *gin.Context) {
	if adminaccess.RestrictedFromContext(c.Request.Context()) != nil {
		response.Forbidden(c, "管理密钥未获此操作权限")
		c.Abort()
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		c.Abort()
		return
	}
	organizationID, ok := parseOrganizationIDParam(c, "organization_id")
	if !ok {
		c.Abort()
		return
	}
	ownerID, err := h.organization.AdminConsoleOwner(c.Request.Context(), subject.UserID, organizationID)
	if err != nil {
		response.ErrorFrom(c, err)
		c.Abort()
		return
	}
	middleware2.SetAuditActor(c, subject.UserID, c.GetString(middleware2.ContextKeyAuthEmail))
	middleware2.SetAuditExtra(c, map[string]any{"organization_id": organizationID, "acting_owner_user_id": ownerID})
	c.Set(AdminConsoleActorContextKey, subject.UserID)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: ownerID, Concurrency: subject.Concurrency})
	c.Next()
}
