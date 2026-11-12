package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// RequestAppFromContext returns client metadata captured at HTTP ingress.
func RequestAppFromContext(ctx context.Context) *string {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Value(ctxkey.XApp).(string)
	return optionalTrimmedStringPtr(value)
}

// RegisterClientInfoFromContext returns the client IP and User-Agent captured
// for public auth requests. Both are empty outside self-registration routes.
func RegisterClientInfoFromContext(ctx context.Context) (string, string) {
	if ctx == nil {
		return "", ""
	}
	ip, _ := ctx.Value(ctxkey.RegisterClientIP).(string)
	ua, _ := ctx.Value(ctxkey.RegisterClientUserAgent).(string)
	return truncateRegisterClientField(strings.TrimSpace(ip), 64), truncateRegisterClientField(strings.TrimSpace(ua), 1024)
}

// WithRegisterClientInfo stores the client metadata used by self-registration.
func WithRegisterClientInfo(ctx context.Context, clientIP, userAgent string) context.Context {
	if ctx == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, ctxkey.RegisterClientIP, strings.TrimSpace(clientIP))
	return context.WithValue(ctx, ctxkey.RegisterClientUserAgent, strings.TrimSpace(userAgent))
}

func truncateRegisterClientField(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func optionalTrimmedStringPtr(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// coalesceRequestedReasoningEffort prefers the client-requested value and falls
// back to the effective/forwarded effort for historical or unmapped rows.
func coalesceRequestedReasoningEffort(requested, forwarded *string) *string {
	if trimmed := optionalStringValue(requested); trimmed != "" {
		return &trimmed
	}
	if trimmed := optionalStringValue(forwarded); trimmed != "" {
		return &trimmed
	}
	return nil
}

func forwardResultBillingModel(requestedModel, upstreamModel string) string {
	if trimmed := strings.TrimSpace(requestedModel); trimmed != "" {
		return normalizeModelNameForPricing(trimmed)
	}
	return normalizeModelNameForPricing(upstreamModel)
}

func optionalInt64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}
