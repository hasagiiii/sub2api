package service

import "context"

// BytedanceExecution is durable state for a synchronous provider behind the task API.
type BytedanceExecution struct {
	TaskID         int64
	RequestPayload map[string]any
	ResultPayload  map[string]any
	State          string
	BillingType    int8
	UnitPrice      float64
	BillableImages *int
	BillingError   string
}

// This optional repository extension leaves existing queue-provider contracts intact.
type BytedanceExecutionRepository interface {
	CreateBytedance(context.Context, *AsyncMediaTask, *BytedanceExecution) error
	GetBytedance(context.Context, int64) (*BytedanceExecution, error)
	ClaimBytedance(context.Context, int64) (bool, error)
	SaveBytedanceResult(context.Context, int64, map[string]any) error
	SettleBytedance(context.Context, *AsyncMediaTask, int, float64, string) (bool, error)
	RefundBytedance(context.Context, int64, string, bool) (bool, error)
}
