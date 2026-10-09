//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

type paymentFulfillmentCostCenterWriterStub struct {
	events []*CreateCostCenterEventInput
}

func (s *paymentFulfillmentCostCenterWriterStub) CreateEvent(_ context.Context, input *CreateCostCenterEventInput) (*CostCenterEvent, error) {
	copyInput := *input
	s.events = append(s.events, &copyInput)
	return &CostCenterEvent{
		EventType:  input.EventType,
		Status:     input.Status,
		SourceType: input.SourceType,
		Category:   input.Category,
		AmountUSD:  input.AmountUSD,
	}, nil
}

func TestMarkCompletedRecordsRechargeBonusAsCostCenterExpense(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)

	user, err := client.User.Create().
		SetEmail("bonus-cost-center@example.com").
		SetPasswordHash("hash").
		SetUsername("bonus-cost-center").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(210).
		SetPayAmount(200).
		SetFeeRate(0).
		SetBonusAmount(10).
		SetRechargeCode("BONUS-COST-CENTER").
		SetOutTradeNo("sub2_bonus_cost_center").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-bonus-cost-center").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusRecharging).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	writer := &paymentFulfillmentCostCenterWriterStub{}
	svc := &PaymentService{entClient: client, costCenter: writer}
	lease := &paymentFulfillmentLease{version: order.UpdatedAt}
	require.NoError(t, svc.markCompleted(ctx, order, lease, "RECHARGE_SUCCESS"))
	require.NoError(t, svc.markCompleted(ctx, order, lease, "RECHARGE_SUCCESS"))

	require.Len(t, writer.events, 2, "a repeated completion must not append duplicate cost-center events")
	require.Equal(t, CostEventIncome, writer.events[0].EventType)

	bonusExpense := writer.events[1]
	require.Equal(t, CostEventExpense, bonusExpense.EventType)
	require.Equal(t, "settled", bonusExpense.Status)
	require.Equal(t, "recharge_bonus", bonusExpense.SourceType)
	require.Equal(t, "recharge_bonus", bonusExpense.Category)
	require.InDelta(t, 10, bonusExpense.AmountUSD, 1e-9)
	require.Equal(t, user.ID, *bonusExpense.UserID)
	require.Equal(t, "payment-order:"+strconv.FormatInt(order.ID, 10)+":recharge-bonus-expense", *bonusExpense.IdempotencyKey)
	require.NotNil(t, bonusExpense.OccurredAt)
}

func TestMarkCompletedWithoutRechargeBonusOnlyRecordsIncome(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	ensurePaymentAuditOrderActionUniqueIndex(t, ctx, client)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusRecharging, time.Now())
	order, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPaid).
		ClearPlanID().
		ClearSubscriptionGroupID().
		ClearSubscriptionDays().
		Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)

	writer := &paymentFulfillmentCostCenterWriterStub{}
	svc := &PaymentService{entClient: client, costCenter: writer}
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NoError(t, svc.markCompleted(ctx, order, lease, "RECHARGE_SUCCESS"))

	require.Len(t, writer.events, 1)
	require.Equal(t, CostEventIncome, writer.events[0].EventType)
}
