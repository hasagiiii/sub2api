package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// SetAccountUserLimitService 注入账号最大用户数服务。
func (h *AccountHandler) SetAccountUserLimitService(limit *service.AccountUserLimitService) {
	if h != nil {
		h.accountUserLimit = limit
	}
}

// GetScheduledUsers 返回进入过该账号的用户、名额占用情况及其对话统计。
// GET /api/v1/admin/accounts/:id/scheduled-users
func (h *AccountHandler) GetScheduledUsers(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result, err := h.accountUserLimit.ListScheduledUsers(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// EvictScheduledUser 清理调度在该账号上的用户：释放名额，允许新用户进入。
// DELETE /api/v1/admin/accounts/:id/scheduled-users/:user_id
func (h *AccountHandler) EvictScheduledUser(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	if _, err := h.adminService.GetAccount(c.Request.Context(), accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	removed, err := h.accountUserLimit.EvictUser(c.Request.Context(), accountID, userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"removed": removed})
}
