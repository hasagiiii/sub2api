//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminConsoleRepositoryStub struct {
	service.OrganizationRepository
	detail          *service.AdminOrganizationDetail
	detailErr       error
	detailActorID   int64
	contextUserID   int64
	organizationCtx *service.OrganizationContext
}

func (s *adminConsoleRepositoryStub) GetOrganization(_ context.Context, actorID, _ int64) (*service.AdminOrganizationDetail, error) {
	s.detailActorID = actorID
	return s.detail, s.detailErr
}

func (s *adminConsoleRepositoryStub) GetContextForUser(_ context.Context, userID int64) (*service.OrganizationContext, error) {
	s.contextUserID = userID
	return s.organizationCtx, nil
}

func newAdminConsoleRouter(repo *adminConsoleRepositoryStub, principal *adminaccess.Principal, seen *servermiddleware.AuthSubject) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewOrganizationHandler(service.NewOrganizationService(repo, nil, &config.Config{}), nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/admin/organizations/:organization_id/console/context",
		func(c *gin.Context) {
			if principal != nil {
				c.Request = c.Request.WithContext(adminaccess.WithPrincipal(c.Request.Context(), principal))
			}
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1})
			c.Next()
		},
		h.AdminActAsOrganizationOwner,
		h.RequireOrganization,
		func(c *gin.Context) {
			subject, _ := servermiddleware.GetAuthSubjectFromContext(c)
			*seen = subject
			actor, _ := c.Get(AdminConsoleActorContextKey)
			c.JSON(http.StatusOK, gin.H{"actor": actor})
		},
	)
	return router
}

func activeOwnerContext() *service.OrganizationContext {
	return &service.OrganizationContext{
		OrganizationID:     5,
		OrganizationStatus: service.OrganizationStatusActive,
		MembershipStatus:   service.MembershipStatusActive,
		Role:               service.OrganizationRoleOwner,
	}
}

func TestAdminActAsOrganizationOwnerRunsConsoleAsTheOwner(t *testing.T) {
	repo := &adminConsoleRepositoryStub{
		detail:          &service.AdminOrganizationDetail{Organization: service.AdminOrganization{ID: 5, OwnerUserID: 9, Status: service.OrganizationStatusActive}},
		organizationCtx: activeOwnerContext(),
	}
	var seen servermiddleware.AuthSubject
	rec := httptest.NewRecorder()
	newAdminConsoleRouter(repo, nil, &seen).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/organizations/5/console/context", nil))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, int64(1), repo.detailActorID, "admin status is re-checked with the real actor")
	require.Equal(t, int64(9), repo.contextUserID, "organization scope is resolved for the owner")
	require.Equal(t, int64(9), seen.UserID)
	require.Contains(t, rec.Body.String(), `"actor":1`)
}

func TestAdminActAsOrganizationOwnerRejectsInactiveOrganizations(t *testing.T) {
	repo := &adminConsoleRepositoryStub{
		detail:          &service.AdminOrganizationDetail{Organization: service.AdminOrganization{ID: 5, OwnerUserID: 9, Status: service.OrganizationStatusSuspended}},
		organizationCtx: activeOwnerContext(),
	}
	var seen servermiddleware.AuthSubject
	rec := httptest.NewRecorder()
	newAdminConsoleRouter(repo, nil, &seen).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/organizations/5/console/context", nil))

	require.Equal(t, http.StatusConflict, rec.Code)
	require.Zero(t, repo.contextUserID)
	require.Zero(t, seen.UserID)
}

func TestAdminActAsOrganizationOwnerPropagatesAdminCheckFailure(t *testing.T) {
	repo := &adminConsoleRepositoryStub{detailErr: service.ErrInsufficientPerms, organizationCtx: activeOwnerContext()}
	var seen servermiddleware.AuthSubject
	rec := httptest.NewRecorder()
	newAdminConsoleRouter(repo, nil, &seen).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/organizations/5/console/context", nil))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Zero(t, repo.contextUserID)
}

func TestAdminActAsOrganizationOwnerRejectsScopedAdminKeys(t *testing.T) {
	repo := &adminConsoleRepositoryStub{
		detail:          &service.AdminOrganizationDetail{Organization: service.AdminOrganization{ID: 5, OwnerUserID: 9, Status: service.OrganizationStatusActive}},
		organizationCtx: activeOwnerContext(),
	}
	var seen servermiddleware.AuthSubject
	rec := httptest.NewRecorder()
	newAdminConsoleRouter(repo, &adminaccess.Principal{KeyID: "k"}, &seen).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/organizations/5/console/context", nil))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Zero(t, repo.detailActorID)
	require.Zero(t, seen.UserID)
}
