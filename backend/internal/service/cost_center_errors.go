package service

import (
	"errors"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrInvalidCostCenterAmount = errors.New("cost center amount must be positive")
var ErrInvalidCostCenterStatus = infraerrors.BadRequest("INVALID_COST_CENTER_STATUS", "invalid cost center event status transition")
var ErrCostCenterReversalDisabled = infraerrors.BadRequest("COST_CENTER_REVERSAL_DISABLED", "creating cost center reversals is disabled")
