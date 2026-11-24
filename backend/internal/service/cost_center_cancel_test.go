package service

import (
	"context"
	"errors"
	"testing"
)

func TestCancelExpenseRequiresReason(t *testing.T) {
	s := NewCostCenterService(&costCenterRepoFake{})
	for _, reason := range []string{"", "  ", "\n\t"} {
		_, err := s.UpdateEventStatus(context.Background(), 9, "cancelled", reason, nil)
		if !errors.Is(err, ErrCostCenterReasonRequired) {
			t.Fatalf("expected reason error, got %v", err)
		}
	}
	_, err := s.UpdateEventStatus(context.Background(), 9, "invalid", "reason", nil)
	if !errors.Is(err, ErrInvalidCostCenterStatus) {
		t.Fatalf("expected status error, got %v", err)
	}
}
