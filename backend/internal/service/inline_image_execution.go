package service

import (
	"context"
	"errors"
)

const InlineImageExecutionOpenAI = "openai_images"

var ErrInlineImageConflict = errors.New("request ID is already used by a different request")
var ErrInlineImageRunning = errors.New("image generation has started and cannot be canceled")

// InlineImageExecution is an internal journal, separate from sanitized usage parameters.
type InlineImageExecution struct {
	TaskID            int64
	RequestPayload    map[string]any
	RequestHash       string
	BillingType       int8
	State             string
	Result            *AsyncMediaInlineResult
	FailureReason     string
	FailureDefinitive bool
}

type InlineImageRepository interface {
	CreateInlineImage(context.Context, *AsyncMediaTask, *InlineImageExecution) error
	GetInlineImage(context.Context, int64) (*InlineImageExecution, error)
	GetInlineImageState(context.Context, int64) (string, error)
	ClaimInlineImage(context.Context, int64, string) (bool, error)
	RenewInlineImage(context.Context, int64, string) (bool, error)
	ReleaseInlineImage(context.Context, int64, string) error
	SaveInlineImageResult(context.Context, int64, string, *AsyncMediaInlineResult, bool) error
	SaveInlineImageFailure(context.Context, int64, string, string, bool) error
	FinishInlineImage(context.Context, *AsyncMediaTask, *TerminalUsageLogInput) (bool, error)
	FailInlineImage(context.Context, int64, string, bool, bool) (bool, error)
}

type InlineImageGenerator func(context.Context, *AsyncMediaTask, *Account, *APIKey, map[string]any) (*AsyncMediaInlineResult, error)
