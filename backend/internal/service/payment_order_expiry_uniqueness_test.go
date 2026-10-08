//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestExpiredPaymentOrderRetainsUniqueOutTradeNo(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("expired-order-unique@example.com").
		SetPasswordHash("hash").
		SetUsername("expired-order-unique").
		Save(ctx)
	require.NoError(t, err)

	const outTradeNo = "sub2_expired_order_unique"
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRED-ORDER-UNIQUE").
		SetOutTradeNo(outTradeNo).
		SetPaymentType("").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	expiredCount, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, expiredCount)

	expiredOrder, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, expiredOrder.Status)
	require.Equal(t, outTradeNo, expiredOrder.OutTradeNo)

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	replacementOutTradeNo, err := svc.allocateOutTradeNo(ctx, tx)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NotEqual(t, outTradeNo, replacementOutTradeNo)

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRED-ORDER-RETRY").
		SetOutTradeNo(outTradeNo).
		SetPaymentType("").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.Error(t, err, "expired order IDs remain unique and cannot be reused")
	require.True(t, dbent.IsConstraintError(err))

	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRED-ORDER-NEW-ID").
		SetOutTradeNo(replacementOutTradeNo).
		SetPaymentType("").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err, "a new external order ID can be created after expiry")
}
