//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestMarkCreatePaymentFailedDoesNotOverwritePaidOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("create-payment-failed@example.com").
		SetPasswordHash("hash").
		SetUsername("create-payment-failed").
		Save(ctx)
	require.NoError(t, err)

	for _, status := range []string{OrderStatusPending, OrderStatusPaid} {
		t.Run(status, func(t *testing.T) {
			order, createErr := client.PaymentOrder.Create().
				SetUserID(user.ID).
				SetUserEmail(user.Email).
				SetUserName(user.Username).
				SetAmount(10).
				SetPayAmount(10).
				SetFeeRate(0).
				SetRechargeCode("CREATE-PAYMENT-FAILED-" + status).
				SetOutTradeNo("sub2_create_payment_failed_" + status).
				SetPaymentType(payment.TypeAlipay).
				SetPaymentTradeNo("").
				SetOrderType(payment.OrderTypeBalance).
				SetStatus(status).
				SetExpiresAt(time.Now().Add(time.Hour)).
				SetClientIP("127.0.0.1").
				SetSrcHost("example.com").
				Save(ctx)
			require.NoError(t, createErr)

			updated, updateErr := (&PaymentService{entClient: client}).markCreatePaymentFailed(ctx, order.ID)
			require.NoError(t, updateErr)
			if status == OrderStatusPending {
				require.Equal(t, 1, updated)
			} else {
				require.Zero(t, updated)
			}

			reloaded, reloadErr := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, reloadErr)
			if status == OrderStatusPending {
				require.Equal(t, OrderStatusFailed, reloaded.Status)
			} else {
				require.Equal(t, OrderStatusPaid, reloaded.Status)
			}
		})
	}
}
