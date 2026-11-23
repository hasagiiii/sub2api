package service

import (
	"errors"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrInvalidCostCenterAmount = errors.New("cost center amount must be positive")
var ErrInvalidCostCenterStatus = infraerrors.BadRequest("COST_CENTER_INVALID_STATUS", "invalid cost center event status")
var ErrCostCenterReasonRequired = infraerrors.BadRequest("COST_CENTER_REASON_REQUIRED", "reason is required")
var ErrCostCenterEventConflict = infraerrors.Conflict("COST_CENTER_EVENT_CONFLICT", "event cannot be changed or reversed in its current state")
var ErrCostCenterEventNotFound = infraerrors.NotFound("COST_CENTER_EVENT_NOT_FOUND", "cost center event not found")
