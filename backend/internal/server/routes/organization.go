package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterOrganizationRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
) {
	if h == nil || h.Organization == nil {
		return
	}
	authenticated := v1.Group("/organization")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	authenticated.Use(gin.HandlerFunc(auditLog))
	{
		authenticated.GET("/applications/current", h.Organization.CurrentApplication)
		authenticated.GET("/applications/eligibility", h.Organization.UpgradeEligibility)
		authenticated.POST("/applications", h.Organization.SubmitApplication)
		authenticated.POST("/applications/:application_id/withdraw", h.Organization.WithdrawApplication)
	}

	organizationScoped := authenticated.Group("")
	organizationScoped.Use(h.Organization.RequireOrganization)
	registerOrganizationConsoleRoutes(organizationScoped, h, true)
}

// registerOrganizationConsoleRoutes registers the enterprise console API. The
// same handlers back the member-facing /organization routes and the system
// administrator's /admin/organizations/:organization_id/console routes, so both
// entry points always expose identical behaviour. personal adds endpoints tied
// to the signed-in account itself (password, recovery email, rename request and
// self-paid subscription orders), which make no sense for an administrator.
func registerOrganizationConsoleRoutes(group *gin.RouterGroup, h *handler.Handlers, personal bool) {
	group.GET("/context", h.Organization.Context)
	if personal {
		group.POST("/name-change-requests", h.Organization.RequestNameChange)
		group.PUT("/password", h.Organization.ChangePassword)
		group.POST("/recovery-email/send-code", h.Organization.SendRecoveryEmailCode)
		group.POST("/recovery-email/verify", h.Organization.VerifyRecoveryEmail)
		group.POST("/subscription-orders", h.Organization.CreateSubscriptionOrder)
	}
	group.GET("/members", h.Organization.ListMembers)
	group.POST("/members", h.Organization.CreateMember)
	group.GET("/members/:member_id", h.Organization.GetMember)
	group.PATCH("/members/:member_id/status", h.Organization.SetMemberStatus)
	group.DELETE("/members/:member_id", h.Organization.DeleteArchivedMember)
	group.POST("/members/:member_id/reset-password", h.Organization.ResetMemberPassword)
	group.GET("/policies", h.Organization.ListPolicies)
	group.GET("/members/:member_id/policies", h.Organization.ListMemberPolicies)
	group.PUT("/members/:member_id/policies", h.Organization.SetPolicy)
	group.GET("/spend-limits", h.Organization.ListSpendLimits)
	group.PUT("/spend-limits", h.Organization.UpsertSpendLimits)
	group.DELETE("/spend-limits", h.Organization.DeleteSpendLimit)
	group.GET("/spend-limits/usage", h.Organization.SpendLimitUsage)
	group.POST("/members/:member_id/balance", h.Organization.TransferBalance)
	group.POST("/company-balance", h.Organization.CompanyBalanceTransfer)
	group.GET("/subscriptions", h.Organization.ListSubscriptions)
	group.GET("/subscription-groups", h.Organization.SubscriptionGroups)
	group.GET("/subscriptions/fallback", h.Organization.SubscriptionFallback)
	group.POST("/subscriptions", h.Organization.CreateSubscription)
	group.DELETE("/subscriptions/:subscription_id", h.Organization.CancelSubscription)
	group.GET("/finance", h.Organization.Finance)
	group.GET("/dashboard", h.Organization.Dashboard)
	group.GET("/dashboard/spending-ranking", h.Organization.SpendingRanking)
	group.GET("/dashboard/user-breakdown", h.Organization.UserBreakdown)
	group.GET("/dashboard/users-trend", h.Organization.UsersTrend)
	group.GET("/usage", h.Organization.Usage)
	group.GET("/usage/:usage_id/video-task", h.Organization.UsageVideoTask)
	group.GET("/usage/api-keys/search", h.Organization.SearchAPIKeys)
	group.GET("/usage/stats", h.Organization.UsageStats)
	group.GET("/usage/trend", h.Organization.UsageTrend)
	group.GET("/usage/charts", h.Organization.UsageCharts)
	group.GET("/usage/errors", h.Organization.UsageErrors)
	group.GET("/usage/errors/:error_id", h.Organization.UsageErrorDetail)
	group.GET("/audit-events", h.Organization.AuditEvents)
	group.GET("/settings", h.Organization.Settings)
	group.PUT("/settings", h.Organization.UpdateSettings)
}
