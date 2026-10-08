//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestExpireTimedOutEasyPayOrderWhenUpstreamQueryIsUnavailable(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("expire-easypay-query-error@example.com").
		SetPasswordHash("hash").
		SetUsername("expire-easypay-query-error").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRE-EASYPAY-QUERY-ERROR").
		SetOutTradeNo("sub2_expire_easypay_query_error").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key:      payment.TypeEasyPay,
		queryErr: errors.New("gateway unavailable"),
	}
	registry.Register(provider)
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	expiredCount, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, expiredCount)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, reloaded.Status)
}

func TestAdminCancelEasyPayOrderWhenUpstreamQueryIsUnavailable(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("admin-cancel-easypay-query-error@example.com").
		SetPasswordHash("hash").
		SetUsername("admin-cancel-easypay-query-error").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("ADMIN-CANCEL-EASYPAY-QUERY-ERROR").
		SetOutTradeNo("sub2_admin_cancel_easypay_query_error").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(&paymentOrderLifecycleQueryProvider{
		key:      payment.TypeEasyPay,
		queryErr: errors.New("gateway unavailable"),
	})
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	outcome, err := svc.AdminCancelOrder(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
}
