// Package handler provides HTTP request handlers for the application.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyHandler handles API key-related requests
type APIKeyHandler struct {
	apiKeyService *service.APIKeyService
}

// NewAPIKeyHandler creates a new APIKeyHandler
func NewAPIKeyHandler(apiKeyService *service.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{
		apiKeyService: apiKeyService,
	}
}

// CreateAPIKeyRequest represents the create API key request payload
type CreateAPIKeyRequest struct {
	Name             string  `json:"name" binding:"required"`
	GroupID          *int64  `json:"group_id"` // nullable
	FallbackGroupIDs []int64 `json:"fallback_group_ids"`
	// OrganizationSubscriptionID 绑定公司订阅，创建企业 API Key（消费走公司订阅）
	OrganizationSubscriptionID *int64 `json:"organization_subscription_id"`
	// UserSubscriptionID 指定扣费套餐（仅决定扣哪份额度池，路由仍看 group_id）。
	// 所指套餐必须覆盖 group_id。
	UserSubscriptionID   *int64   `json:"user_subscription_id"`
	PreferCompanyBalance bool     `json:"prefer_company_balance"`
	CustomKey            *string  `json:"custom_key"`      // 可选的自定义key
	IPWhitelist          []string `json:"ip_whitelist"`    // IP 白名单
	IPBlacklist          []string `json:"ip_blacklist"`    // IP 黑名单
	Quota                *float64 `json:"quota"`           // 配额限制 (USD)
	ExpiresInDays        *int     `json:"expires_in_days"` // 过期天数

	// Rate limit fields (0 = unlimited)
	RateLimit5h *float64 `json:"rate_limit_5h"`
	RateLimit1d *float64 `json:"rate_limit_1d"`
	RateLimit7d *float64 `json:"rate_limit_7d"`
}

// UpdateAPIKeyRequest represents the update API key request payload
type UpdateAPIKeyRequest struct {
	Name             string   `json:"name"`
	GroupID          *int64   `json:"group_id"`
	GroupIDSet       bool     `json:"-"`
	FallbackGroupIDs *[]int64 `json:"fallback_group_ids"`
	// OrganizationSubscriptionID 重新绑定公司订阅（企业 API Key）
	OrganizationSubscriptionID *int64 `json:"organization_subscription_id"`
	// UserSubscriptionID 指定扣费套餐；与 group_id 一同提交，省略即取消指定。
	UserSubscriptionID   *int64    `json:"user_subscription_id"`
	PreferCompanyBalance *bool     `json:"prefer_company_balance"`
	Status               string    `json:"status" binding:"omitempty,oneof=active inactive"`
	IPWhitelist          *[]string `json:"ip_whitelist"` // IP 白名单（nil 不修改，空数组清空）
	IPBlacklist          *[]string `json:"ip_blacklist"` // IP 黑名单（nil 不修改，空数组清空）
	Quota                *float64  `json:"quota"`        // 配额限制 (USD), 0=无限制
	ExpiresAt            *string   `json:"expires_at"`   // 过期时间 (ISO 8601)
	ResetQuota           *bool     `json:"reset_quota"`  // 重置已用配额

	// Rate limit fields (nil = no change, 0 = unlimited)
	RateLimit5h         *float64 `json:"rate_limit_5h"`
	RateLimit1d         *float64 `json:"rate_limit_1d"`
	RateLimit7d         *float64 `json:"rate_limit_7d"`
	ResetRateLimitUsage *bool    `json:"reset_rate_limit_usage"` // 重置限速用量
}

// UnmarshalJSON records whether group_id was present in the request. A nil
// pointer alone cannot distinguish an omitted field from an explicit null,
// which is needed to select automatic routing for a plan-bound key.
func (r *UpdateAPIKeyRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateAPIKeyRequest
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = UpdateAPIKeyRequest(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, r.GroupIDSet = fields["group_id"]
	return nil
}

func validAPIKeyLimit(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

func validateAPIKeyCreateRequest(req CreateAPIKeyRequest) error {
	if req.Quota != nil && !validAPIKeyLimit(*req.Quota) {
		return errors.New("invalid quota")
	}
	if req.RateLimit5h != nil && !validAPIKeyLimit(*req.RateLimit5h) {
		return errors.New("invalid rate_limit_5h")
	}
	if req.RateLimit1d != nil && !validAPIKeyLimit(*req.RateLimit1d) {
		return errors.New("invalid rate_limit_1d")
	}
	if req.RateLimit7d != nil && !validAPIKeyLimit(*req.RateLimit7d) {
		return errors.New("invalid rate_limit_7d")
	}
	if req.ExpiresInDays != nil && *req.ExpiresInDays <= 0 {
		return errors.New("invalid expires_in_days")
	}
	return nil
}

func validateAPIKeyUpdateRequest(req UpdateAPIKeyRequest) error {
	if req.Quota != nil && !validAPIKeyLimit(*req.Quota) {
		return errors.New("invalid quota")
	}
	if req.RateLimit5h != nil && !validAPIKeyLimit(*req.RateLimit5h) {
		return errors.New("invalid rate_limit_5h")
	}
	if req.RateLimit1d != nil && !validAPIKeyLimit(*req.RateLimit1d) {
		return errors.New("invalid rate_limit_1d")
	}
	if req.RateLimit7d != nil && !validAPIKeyLimit(*req.RateLimit7d) {
		return errors.New("invalid rate_limit_7d")
	}
	return nil
}

// List handles listing user's API keys with pagination
// GET /api/v1/api-keys
func (h *APIKeyHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	// Parse filter parameters
	var filters service.APIKeyListFilters
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		if len(search) > 100 {
			search = search[:100]
		}
		filters.Search = search
	}
	filters.Status = c.Query("status")
	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		gid, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err == nil {
			filters.GroupID = &gid
		}
	}

	keys, result, err := h.apiKeyService.List(c.Request.Context(), subject.UserID, params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.APIKey, 0, len(keys))
	for i := range keys {
		out = append(out, *dto.APIKeyFromService(&keys[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

// GetByID handles getting a single API key
// GET /api/v1/api-keys/:id
func (h *APIKeyHandler) GetByID(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 验证所有权
	if key.UserID != subject.UserID {
		response.NotFound(c, "API key not found")
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Create handles creating a new API key
// POST /api/v1/api-keys
func (h *APIKeyHandler) Create(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	var req CreateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := validateAPIKeyCreateRequest(req); err != nil {
		response.BadRequest(c, "Invalid request: numeric limits must be finite and non-negative, and expires_in_days must be greater than zero")
		return
	}

	svcReq := service.CreateAPIKeyRequest{
		Name:                       req.Name,
		GroupID:                    req.GroupID,
		FallbackGroupIDs:           req.FallbackGroupIDs,
		OrganizationSubscriptionID: req.OrganizationSubscriptionID,
		UserSubscriptionID:         req.UserSubscriptionID,
		PreferCompanyBalance:       req.PreferCompanyBalance,
		CustomKey:                  req.CustomKey,
		IPWhitelist:                req.IPWhitelist,
		IPBlacklist:                req.IPBlacklist,
		ExpiresInDays:              req.ExpiresInDays,
	}
	if req.Quota != nil {
		svcReq.Quota = *req.Quota
	}
	if req.RateLimit5h != nil {
		svcReq.RateLimit5h = *req.RateLimit5h
	}
	if req.RateLimit1d != nil {
		svcReq.RateLimit1d = *req.RateLimit1d
	}
	if req.RateLimit7d != nil {
		svcReq.RateLimit7d = *req.RateLimit7d
	}

	executeUserIdempotentJSON(c, "user.api_keys.create", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		key, err := h.apiKeyService.Create(ctx, subject.UserID, svcReq)
		if err != nil {
			return nil, err
		}
		return dto.APIKeyFromService(key), nil
	})
}

// Update handles updating an API key
// PUT /api/v1/api-keys/:id
func (h *APIKeyHandler) Update(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	var req UpdateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := validateAPIKeyUpdateRequest(req); err != nil {
		response.BadRequest(c, "Invalid request: numeric limits must be finite and non-negative")
		return
	}

	svcReq := service.UpdateAPIKeyRequest{
		IPWhitelist:         req.IPWhitelist,
		IPBlacklist:         req.IPBlacklist,
		FallbackGroupIDs:    req.FallbackGroupIDs,
		Quota:               req.Quota,
		ResetQuota:          req.ResetQuota,
		RateLimit5h:         req.RateLimit5h,
		RateLimit1d:         req.RateLimit1d,
		RateLimit7d:         req.RateLimit7d,
		ResetRateLimitUsage: req.ResetRateLimitUsage,
	}
	if req.Name != "" {
		svcReq.Name = &req.Name
	}
	svcReq.GroupID = req.GroupID
	svcReq.GroupIDSet = req.GroupIDSet
	svcReq.OrganizationSubscriptionID = req.OrganizationSubscriptionID
	svcReq.UserSubscriptionID = req.UserSubscriptionID
	svcReq.PreferCompanyBalance = req.PreferCompanyBalance
	if req.Status != "" {
		svcReq.Status = &req.Status
	}
	// Parse expires_at if provided
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			// Empty string means clear expiration
			svcReq.ExpiresAt = nil
			svcReq.ClearExpiration = true
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				response.BadRequest(c, "Invalid expires_at format: "+err.Error())
				return
			}
			svcReq.ExpiresAt = &t
		}
	}

	key, err := h.apiKeyService.Update(c.Request.Context(), keyID, subject.UserID, svcReq)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.APIKeyFromService(key))
}

// Delete handles deleting an API key
// DELETE /api/v1/api-keys/:id
func (h *APIKeyHandler) Delete(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	keyID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid key ID")
		return
	}

	err = h.apiKeyService.Delete(c.Request.Context(), keyID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"message": "API key deleted successfully"})
}

// GetAvailableGroups 获取用户可以绑定的分组列表
// GET /api/v1/groups/available
func (h *APIKeyHandler) GetAvailableGroups(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.Group, 0, len(groups))
	for i := range groups {
		out = append(out, *dto.GroupFromService(&groups[i]))
	}
	response.Success(c, out)
}

// GetBindableOrganizationSubscriptions 获取当前用户（作为组织成员）可绑定的活跃公司订阅。
// 前端据此在创建企业 API Key 时展示"公司订阅分组"可选项。
// GET /api/v1/api-keys/organization-subscriptions
func (h *APIKeyHandler) GetBindableOrganizationSubscriptions(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	subs, err := h.apiKeyService.ListBindableOrganizationSubscriptions(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, gin.H{"subscriptions": subs})
}

// BindableUserSubscription 是"选择扣费套餐"选择器所需的最小信息。
type BindableUserSubscription struct {
	ID int64 `json:"id"`
	// PlanID 为 nil 表示后台手动分配、不属于任何套餐的订阅。
	PlanID *int64 `json:"plan_id,omitempty"`
	// PlanName 是来源套餐的展示名称；手动分配的订阅为空。
	PlanName string `json:"plan_name,omitempty"`
	// GroupID 是主分组；GroupIDs 是共享这份额度的全部分组。
	GroupID  int64   `json:"group_id"`
	GroupIDs []int64 `json:"group_ids"`
	// GroupNames 与 GroupIDs 一一对应。
	//
	// 名称随接口一起返回，而不是交给前端用它已加载的分组列表去映射：那个列表只含
	// 用户【可绑定】的分组，而订阅覆盖的分组不一定都在其中（例如某个专属分组并未
	// 授予该用户），映射不到就只能显示成 #id。
	GroupNames []string  `json:"group_names"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// GetBindableUserSubscriptions 获取当前用户可绑定到 API Key 的活跃个人订阅。
// 前端据此让用户"选套餐"而不是"选分组"。
// GET /api/v1/api-keys/user-subscriptions
func (h *APIKeyHandler) GetBindableUserSubscriptions(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	subs, err := h.apiKeyService.ListBindableUserSubscriptions(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]BindableUserSubscription, 0, len(subs))
	groupNames := h.apiKeyService.SubscriptionGroupNames(c.Request.Context(), subs)
	for i := range subs {
		sub := &subs[i]
		groupIDs := sub.CoveredGroupIDs()
		names := make([]string, 0, len(groupIDs))
		for _, groupID := range groupIDs {
			names = append(names, groupNames[groupID])
		}
		out = append(out, BindableUserSubscription{
			ID:         sub.ID,
			PlanID:     sub.PlanID,
			PlanName:   sub.PlanName,
			GroupID:    sub.GroupID,
			GroupIDs:   groupIDs,
			GroupNames: names,
			ExpiresAt:  sub.ExpiresAt,
		})
	}
	response.Success(c, gin.H{"subscriptions": out})
}

// GetUserGroupRates 获取当前用户的专属分组倍率配置
// GET /api/v1/groups/rates
func (h *APIKeyHandler) GetUserGroupRates(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	rates, err := h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, rates)
}
