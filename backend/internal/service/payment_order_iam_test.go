package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestShouldBlockIAMFinancialOperation(t *testing.T) {
	tests := []struct {
		name string
		user *User
		req  CreateOrderRequest
		want bool
	}{
		{
			name: "enterprise subscription order is allowed after organization authorization",
			user: &User{IdentityType: IdentityTypeIAM},
			req: CreateOrderRequest{
				OrderType:      payment.OrderTypeSubscription,
				OrganizationID: 42,
			},
			want: false,
		},
		{
			name: "personal balance order remains blocked",
			user: &User{IdentityType: IdentityTypeIAM},
			req:  CreateOrderRequest{OrderType: payment.OrderTypeBalance},
			want: true,
		},
		{
			name: "subscription without organization remains blocked",
			user: &User{IdentityType: IdentityTypeIAM},
			req:  CreateOrderRequest{OrderType: payment.OrderTypeSubscription},
			want: true,
		},
		{
			name: "root user is not blocked",
			user: &User{IdentityType: IdentityTypeRoot},
			req:  CreateOrderRequest{OrderType: payment.OrderTypeBalance},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldBlockIAMFinancialOperation(tt.user, tt.req))
		})
	}
}
