//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

type paymentConfirmationProviderStub struct {
	response       *payment.QueryOrderResponse
	queryErr       error
	queryCalls     int
	lastQueryOrder string
}

func (p *paymentConfirmationProviderStub) Name() string { return "payment-confirmation-stub" }
func (p *paymentConfirmationProviderStub) ProviderKey() string {
	return payment.TypeEasyPay
}
func (p *paymentConfirmationProviderStub) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeEasyPay}
}
func (p *paymentConfirmationProviderStub) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}
func (p *paymentConfirmationProviderStub) QueryOrder(_ context.Context, orderID string) (*payment.QueryOrderResponse, error) {
	p.queryCalls++
	p.lastQueryOrder = orderID
	return p.response, p.queryErr
}
func (p *paymentConfirmationProviderStub) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}
func (p *paymentConfirmationProviderStub) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}
func (p *paymentConfirmationProviderStub) MerchantIdentityMetadata() map[string]string {
	return map[string]string{"pid": "merchant-1", "gateway_identity": "gateway-1"}
}

type paymentConfirmationTestEnv struct {
	ctx        context.Context
	client     *dbent.Client
	order      *dbent.PaymentOrder
	userRepo   *mockUserRepo
	redeemRepo *paymentOrderLifecycleRedeemRepo
	provider   *paymentConfirmationProviderStub
	service    *PaymentService
	risk       *PaymentRiskService
}

func newPaymentConfirmationTestEnv(t *testing.T) *paymentConfirmationTestEnv {
	return newPaymentConfirmationTestEnvWithOptions(t, false)
}

func newPaymentConfirmationTestEnvWithRisk(t *testing.T) *paymentConfirmationTestEnv {
	return newPaymentConfirmationTestEnvWithOptions(t, true)
}

func newPaymentConfirmationTestEnvWithOptions(t *testing.T, withRisk bool) *paymentConfirmationTestEnv {
	t.Helper()

	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	user, err := client.User.Create().
		SetEmail("payment-confirmation-" + suffix + "@example.com").
		SetPasswordHash("hash").
		SetUsername("payment-confirmation-" + suffix).
		SetBalance(10).
		Save(ctx)
	require.NoError(t, err)

	var riskService *PaymentRiskService
	clientIP := "127.0.0.1"
	if withRisk {
		migration, migrationErr := migrations.FS.ReadFile("244_payment_risk_ip_policy.sql")
		require.NoError(t, migrationErr)
		_, migrationErr = client.ExecContext(ctx, string(migration))
		require.NoError(t, migrationErr)
		riskService = NewPaymentRiskService(client, nil)
		riskService.now = func() time.Time { return time.Now().Add(-time.Minute) }
		clientIP = "8.8.8.8"
		require.NoError(t, riskService.RecordUserIP(ctx, user.ID, clientIP, "registration"))
	}

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("PAYMENT-CONFIRMATION-" + suffix).
		SetOutTradeNo("sub2_payment_confirmation_" + suffix).
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("").
		SetProviderGatewayIdentity("gateway-1").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP(clientIP).
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{ID: user.ID, Email: user.Email, Username: user.Username, Balance: 10},
	}
	userRepo.updateBalanceFn = func(_ context.Context, userID int64, amount float64) error {
		require.Equal(t, user.ID, userID)
		userRepo.getByIDUser.Balance += amount
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID: 1, Code: order.RechargeCode, Type: RedeemTypeBalance, Value: order.Amount, Status: StatusUnused,
			},
		},
	}
	provider := &paymentConfirmationProviderStub{}
	registry := payment.NewRegistry()
	registry.Register(provider)
	service := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   NewRedeemService(redeemRepo, userRepo, nil, nil, nil, client, nil, nil),
		userRepo:        userRepo,
		providersLoaded: true,
	}
	if riskService != nil {
		service.SetRiskService(riskService)
	}

	return &paymentConfirmationTestEnv{
		ctx: ctx, client: client, order: order, userRepo: userRepo,
		redeemRepo: redeemRepo, provider: provider, service: service, risk: riskService,
	}
}

func (e *paymentConfirmationTestEnv) notification(tradeNo string) *payment.PaymentNotification {
	return &payment.PaymentNotification{
		TradeNo: tradeNo,
		OrderID: e.order.OutTradeNo,
		Amount:  e.order.PayAmount,
		Status:  payment.NotificationStatusSuccess,
		Metadata: map[string]string{
			"pid": "merchant-1",
		},
	}
}

func (e *paymentConfirmationTestEnv) paidQuery() *payment.QueryOrderResponse {
	return &payment.QueryOrderResponse{
		TradeNo: "gateway-transaction-1",
		Status:  payment.ProviderStatusPaid,
		Amount:  e.order.PayAmount,
		Metadata: map[string]string{
			"pid":          "merchant-1",
			"out_trade_no": e.order.OutTradeNo,
		},
	}
}

func TestEasyPayNotificationRejectsMissingCallbackTradeNumberBeforeQuery(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = env.paidQuery()

	err := env.service.HandlePaymentNotification(env.ctx, env.notification(" "), payment.TypeEasyPay)
	require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
	require.Zero(t, env.provider.queryCalls)
	require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
	require.Empty(t, env.redeemRepo.useCalls)

	order, err := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, order.Status)
	require.Empty(t, order.PaymentTradeNo)
}

func TestEasyPayNotificationFailsClosedOnQueryAndIdentityMismatches(t *testing.T) {
	tests := []struct {
		name      string
		queryErr  error
		mutate    func(*paymentConfirmationTestEnv, *payment.PaymentNotification)
		wantCalls int
	}{
		{name: "query error", queryErr: errors.New("provider credentials must not reach audit")},
		{
			name: "unpaid query",
			mutate: func(env *paymentConfirmationTestEnv, _ *payment.PaymentNotification) {
				env.provider.response.Status = payment.ProviderStatusPending
			},
		},
		{
			name: "amount differs by one cent",
			mutate: func(env *paymentConfirmationTestEnv, _ *payment.PaymentNotification) {
				env.provider.response.Amount = 88.01
			},
		},
		{
			name: "fractional cent",
			mutate: func(env *paymentConfirmationTestEnv, notification *payment.PaymentNotification) {
				notification.Amount = 88.001
				env.provider.response.Amount = 88.001
			},
		},
		{
			name: "query returns another order",
			mutate: func(env *paymentConfirmationTestEnv, _ *payment.PaymentNotification) {
				env.provider.response.Metadata["out_trade_no"] = "sub2_different_order"
			},
		},
		{
			name: "query returns another merchant",
			mutate: func(env *paymentConfirmationTestEnv, _ *payment.PaymentNotification) {
				env.provider.response.Metadata["pid"] = "merchant-2"
			},
		},
		{
			name: "query transaction differs from callback",
			mutate: func(env *paymentConfirmationTestEnv, _ *payment.PaymentNotification) {
				env.provider.response.TradeNo = "gateway-transaction-2"
			},
		},
		{
			name: "query omits transaction",
			mutate: func(env *paymentConfirmationTestEnv, _ *payment.PaymentNotification) {
				env.provider.response.TradeNo = ""
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newPaymentConfirmationTestEnv(t)
			env.provider.response = env.paidQuery()
			env.provider.queryErr = test.queryErr
			notification := env.notification("gateway-transaction-1")
			if test.mutate != nil {
				test.mutate(env, notification)
			}

			err := env.service.HandlePaymentNotification(env.ctx, notification, payment.TypeEasyPay)
			require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
			require.Equal(t, 1, env.provider.queryCalls)
			require.Equal(t, env.order.OutTradeNo, env.provider.lastQueryOrder)
			require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
			require.Empty(t, env.redeemRepo.useCalls)

			order, loadErr := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
			require.NoError(t, loadErr)
			require.Equal(t, OrderStatusPending, order.Status)
			require.Empty(t, order.PaymentTradeNo)
			if test.name == "query error" {
				audits, auditErr := env.client.PaymentAuditLog.Query().
					Where(
						paymentauditlog.OrderIDEQ(strconv.FormatInt(env.order.ID, 10)),
						paymentauditlog.ActionEQ("PAYMENT_GATEWAY_CONFIRMATION_FAILED"),
					).
					All(env.ctx)
				require.NoError(t, auditErr)
				require.Len(t, audits, 1)
				require.NotContains(t, audits[0].Detail, "provider credentials")
			}
		})
	}
}

func TestEasyPayNotificationUsesAuthoritativeQueryAndReplayIsIdempotent(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = env.paidQuery()
	notification := env.notification("gateway-transaction-1")

	_, providerErr := env.service.getOrderProvider(env.ctx, env.order)
	require.NoError(t, providerErr)
	require.NoError(t, env.service.HandlePaymentNotification(env.ctx, notification, payment.TypeEasyPay))
	require.NoError(t, env.service.HandlePaymentNotification(env.ctx, notification, payment.TypeEasyPay))

	order, err := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, order.Status)
	require.Equal(t, "gateway-transaction-1", order.PaymentTradeNo)
	require.Equal(t, 3, env.provider.queryCalls)
	require.Equal(t, 98.0, env.userRepo.getByIDUser.Balance)
	require.Len(t, env.redeemRepo.useCalls, 1)
}

func TestEasyPayVerifiedRSA2QueryAmountMismatchRecordsEvidenceAndBlocksTrustedIP(t *testing.T) {
	env := newPaymentConfirmationTestEnvWithRisk(t)
	query := env.paidQuery()
	query.Amount = env.order.PayAmount - 1
	query.SignatureVerified = true
	query.Metadata["gateway_sign_type"] = "RSA2"
	query.Metadata["gateway_key_id"] = "test-key-1"
	query.Metadata["gateway_public_key_sha256"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	env.provider.response = query

	err := env.service.HandlePaymentNotification(env.ctx, env.notification("gateway-transaction-1"), payment.TypeEasyPay)
	require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
	require.ErrorIs(t, env.risk.CheckIP(env.ctx, "8.8.8.8"), ErrPaymentRiskIPBlocked)
	account, err := env.client.User.Get(env.ctx, env.order.UserID)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, account.Status)

	audits, err := env.service.GetOrderAuditLogs(env.ctx, env.order.ID)
	require.NoError(t, err)
	var evidence map[string]any
	for _, audit := range audits {
		if audit.Action == "PAYMENT_RISK_CONFIRMED" {
			require.NoError(t, json.Unmarshal([]byte(audit.Detail), &evidence))
		}
	}
	require.Equal(t, float64(env.order.ID), evidence["order_id"])
	require.Equal(t, env.order.PayAmount, evidence["expected_pay_amount"])
	require.Equal(t, query.Amount, evidence["queried_amount"])
	require.Equal(t, env.order.OutTradeNo, evidence["query_out_trade_no"])
	require.Equal(t, "merchant-1", evidence["merchant"])
	require.Equal(t, "gateway-transaction-1", evidence["trade_no"])
	require.Equal(t, "test-key-1", evidence["gateway_key_id"])
	require.Equal(t, true, evidence["signatureVerified"])
	var auditData map[string]any
	require.NoError(t, json.Unmarshal([]byte(auditDetail(t, audits, "PAYMENT_RISK_CONFIRMED")), &auditData))
	require.NotContains(t, auditData, "pkey")
	require.NotContains(t, auditData, "sign")
}

func TestEasyPayCallbackAmountMismatchDoesNotTriggerRiskBlock(t *testing.T) {
	env := newPaymentConfirmationTestEnvWithRisk(t)
	env.provider.response = env.paidQuery()
	notification := env.notification("gateway-transaction-1")
	notification.Amount--

	err := env.service.HandlePaymentNotification(env.ctx, notification, payment.TypeEasyPay)
	require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
	require.NoError(t, env.risk.CheckIP(env.ctx, "8.8.8.8"))
	account, err := env.client.User.Get(env.ctx, env.order.UserID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, account.Status)
	audits, err := env.service.GetOrderAuditLogs(env.ctx, env.order.ID)
	require.NoError(t, err)
	for _, audit := range audits {
		require.NotEqual(t, "PAYMENT_RISK_CONFIRMED", audit.Action)
	}
}

func TestEasyPayAdminRetryRequiresFreshGatewayConfirmation(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = &payment.QueryOrderResponse{
		Status: payment.ProviderStatusPending,
	}
	paidAt := time.Now().Add(-time.Minute)
	order, err := env.client.PaymentOrder.UpdateOneID(env.order.ID).
		SetStatus(OrderStatusPaid).
		SetPaidAt(paidAt).
		Save(env.ctx)
	require.NoError(t, err)

	err = env.service.RetryFulfillment(env.ctx, order.ID)
	require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
	require.Equal(t, 1, env.provider.queryCalls)
	require.Equal(t, order.OutTradeNo, env.provider.lastQueryOrder)
	require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
	require.Empty(t, env.redeemRepo.useCalls)

	reloaded, err := env.client.PaymentOrder.Get(env.ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
	require.Empty(t, reloaded.PaymentTradeNo)
}

func TestEasyPayConcurrentPaidUpdatesCannotReuseMerchantTransaction(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}
	secondOrder, err := env.client.PaymentOrder.Create().
		SetUserID(env.order.UserID).
		SetUserEmail(env.order.UserEmail).
		SetUserName(env.order.UserName).
		SetAmount(env.order.Amount).
		SetPayAmount(env.order.PayAmount).
		SetFeeRate(0).
		SetRechargeCode(env.order.RechargeCode + "-second").
		SetOutTradeNo(env.order.OutTradeNo + "-second").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(env.ctx)
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, order := range []*dbent.PaymentOrder{env.order, secondOrder} {
		order := order
		go func() {
			<-start
			errs <- env.service.toPaid(env.ctx, order, "shared-gateway-transaction", order.PayAmount, payment.TypeEasyPay, "merchant-1", "gateway-1")
		}()
	}
	close(start)
	firstErr := <-errs
	secondErr := <-errs
	require.ErrorIs(t, firstErr, ErrPaymentGatewayConfirmation)
	require.ErrorIs(t, secondErr, ErrPaymentGatewayConfirmation)
	require.ElementsMatch(t, []string{
		"payment gateway confirmation failed: transaction_already_bound",
		"payment gateway confirmation failed: gateway_order_not_paid",
	}, []string{firstErr.Error(), secondErr.Error()})

	firstOrder, err := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
	require.NoError(t, err)
	secondOrder, err = env.client.PaymentOrder.Get(env.ctx, secondOrder.ID)
	require.NoError(t, err)
	boundCount := 0
	for _, order := range []*dbent.PaymentOrder{firstOrder, secondOrder} {
		if order.PaymentTradeNo == "shared-gateway-transaction" {
			boundCount++
			require.Equal(t, "merchant-1", valueOrEmpty(order.ProviderMerchantID))
		}
	}
	require.Equal(t, 1, boundCount)
	require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
	require.Empty(t, env.redeemRepo.useCalls)
}

func TestEasyPayTransactionIdentityUniqueIndexIsGatewayAndMerchantScoped(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}
	secondOrder, err := env.client.PaymentOrder.Create().
		SetUserID(env.order.UserID).
		SetUserEmail(env.order.UserEmail).
		SetUserName(env.order.UserName).
		SetAmount(env.order.Amount).
		SetPayAmount(env.order.PayAmount).
		SetFeeRate(0).
		SetRechargeCode(env.order.RechargeCode + "-other-merchant").
		SetOutTradeNo(env.order.OutTradeNo + "-other-merchant").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("").
		SetProviderGatewayIdentity("gateway-1").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(env.ctx)
	require.NoError(t, err)

	firstErr := env.service.toPaid(env.ctx, env.order, "shared-gateway-transaction", env.order.PayAmount, payment.TypeEasyPay, "merchant-1", "gateway-1")
	require.ErrorIs(t, firstErr, ErrPaymentGatewayConfirmation)
	require.Contains(t, firstErr.Error(), "gateway_order_not_paid")
	secondErr := env.service.toPaid(env.ctx, secondOrder, "shared-gateway-transaction", secondOrder.PayAmount, payment.TypeEasyPay, "merchant-2", "gateway-1")
	require.ErrorIs(t, secondErr, ErrPaymentGatewayConfirmation)
	require.NotContains(t, secondErr.Error(), "transaction_already_bound")

	firstOrder, err := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
	require.NoError(t, err)
	secondOrder, err = env.client.PaymentOrder.Get(env.ctx, secondOrder.ID)
	require.NoError(t, err)
	require.Equal(t, "shared-gateway-transaction", firstOrder.PaymentTradeNo)
	require.Equal(t, "merchant-1", valueOrEmpty(firstOrder.ProviderMerchantID))
	require.Equal(t, "shared-gateway-transaction", secondOrder.PaymentTradeNo)
	require.Equal(t, "merchant-2", valueOrEmpty(secondOrder.ProviderMerchantID))
	require.Equal(t, OrderStatusPaid, firstOrder.Status)
	require.Equal(t, OrderStatusPaid, secondOrder.Status)
	require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
}

func TestEasyPayDirectFulfillmentRequiresFreshGatewayConfirmation(t *testing.T) {
	tests := []struct {
		name string
		run  func(*paymentConfirmationTestEnv) error
	}{
		{
			name: "balance",
			run: func(env *paymentConfirmationTestEnv) error {
				return env.service.ExecuteBalanceFulfillment(env.ctx, env.order.ID)
			},
		},
		{
			name: "subscription",
			run: func(env *paymentConfirmationTestEnv) error {
				_, err := env.client.PaymentOrder.UpdateOneID(env.order.ID).
					SetOrderType(payment.OrderTypeSubscription).
					SetPlanID(1).
					SetSubscriptionGroupID(1).
					SetSubscriptionDays(30).
					Save(env.ctx)
				if err != nil {
					return err
				}
				return env.service.ExecuteSubscriptionFulfillment(env.ctx, env.order.ID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newPaymentConfirmationTestEnv(t)
			env.provider.response = &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}
			_, err := env.client.PaymentOrder.UpdateOneID(env.order.ID).
				SetStatus(OrderStatusPaid).
				SetPaidAt(time.Now().Add(-time.Minute)).
				Save(env.ctx)
			require.NoError(t, err)

			err = test.run(env)
			require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
			require.Equal(t, 1, env.provider.queryCalls)
			require.Equal(t, env.order.OutTradeNo, env.provider.lastQueryOrder)
			require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
			require.Empty(t, env.redeemRepo.useCalls)

			reloaded, loadErr := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
			require.NoError(t, loadErr)
			require.Equal(t, OrderStatusPaid, reloaded.Status)
			require.Empty(t, reloaded.PaymentTradeNo)
		})
	}
}

func TestEasyPayNotificationRejectsTransactionAlreadyBoundToAnotherOrder(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = env.paidQuery()
	_, err := env.client.PaymentOrder.Create().
		SetUserID(env.order.UserID).
		SetUserEmail(env.order.UserEmail).
		SetUserName(env.order.UserName).
		SetAmount(88).
		SetPayAmount(88).
		SetRechargeCode(env.order.RechargeCode + "-other").
		SetOutTradeNo(env.order.OutTradeNo + "-other").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("gateway-transaction-1").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(env.ctx)
	require.NoError(t, err)

	err = env.service.HandlePaymentNotification(env.ctx, env.notification("gateway-transaction-1"), payment.TypeEasyPay)
	require.ErrorIs(t, err, ErrPaymentGatewayConfirmation)
	require.Equal(t, 10.0, env.userRepo.getByIDUser.Balance)
	require.Empty(t, env.redeemRepo.useCalls)

	order, err := env.client.PaymentOrder.Get(env.ctx, env.order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, order.Status)
	require.Empty(t, order.PaymentTradeNo)
}

func TestEasyPayNotificationAllowsSameTransactionReferenceForDifferentGateways(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	env.provider.response = env.paidQuery()
	_, err := env.client.PaymentOrder.Create().
		SetUserID(env.order.UserID).
		SetUserEmail(env.order.UserEmail).
		SetUserName(env.order.UserName).
		SetAmount(88).
		SetPayAmount(88).
		SetRechargeCode(env.order.RechargeCode + "-other-gateway").
		SetOutTradeNo(env.order.OutTradeNo + "-other-gateway").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("gateway-transaction-1").
		SetProviderKey(payment.TypeEasyPay).
		SetProviderMerchantID("merchant-1").
		SetProviderGatewayIdentity("gateway-2").
		SetProviderSnapshot(map[string]any{
			"schema_version":   3,
			"provider_key":     payment.TypeEasyPay,
			"merchant_id":      "merchant-1",
			"gateway_identity": "gateway-2",
		}).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(env.ctx)
	require.NoError(t, err)

	require.NoError(t, env.service.HandlePaymentNotification(env.ctx, env.notification("gateway-transaction-1"), payment.TypeEasyPay))
	require.Equal(t, 98.0, env.userRepo.getByIDUser.Balance)
	require.Len(t, env.redeemRepo.useCalls, 1)
}

func TestConfirmedLegacyEasyPayProviderIdentityCannotDrift(t *testing.T) {
	env := newPaymentConfirmationTestEnv(t)
	order, err := env.client.PaymentOrder.UpdateOneID(env.order.ID).
		SetProviderKey(payment.TypeEasyPay).SetProviderMerchantID("merchant-1").
		SetProviderGatewayIdentity("gateway-1").SetPaymentTradeNo("gateway-transaction-1").
		SetPaidAt(time.Now()).SetStatus(OrderStatusPaid).Save(env.ctx)
	require.NoError(t, err)
	resolved, err := env.service.getOrderProvider(env.ctx, order)
	require.NoError(t, err)
	require.Same(t, env.provider, resolved)

	for _, mutation := range []func(*dbent.PaymentOrder){
		func(changed *dbent.PaymentOrder) { value := "another-merchant"; changed.ProviderMerchantID = &value },
		func(changed *dbent.PaymentOrder) {
			value := "another-gateway"
			changed.ProviderGatewayIdentity = &value
		},
		func(changed *dbent.PaymentOrder) { changed.PaidAt = nil },
		func(changed *dbent.PaymentOrder) { changed.Status = OrderStatusPending },
		func(changed *dbent.PaymentOrder) { changed.PaymentTradeNo = "" },
		func(changed *dbent.PaymentOrder) { value := "missing-instance"; changed.ProviderInstanceID = &value },
		func(changed *dbent.PaymentOrder) {
			changed.ProviderSnapshot = map[string]any{"schema_version": 3, "provider_instance_id": "404"}
		},
	} {
		changed := *order
		mutation(&changed)
		_, err := env.service.getOrderProvider(env.ctx, &changed)
		require.Error(t, err)
	}
}

func TestPaymentAmountToCentsRequiresExactPositiveCents(t *testing.T) {
	for _, test := range []struct {
		value float64
		want  int64
		ok    bool
	}{
		{value: 10, want: 1000, ok: true},
		{value: 10.01, want: 1001, ok: true},
		{value: 0.01, want: 1, ok: true},
		{value: 10.001},
		{value: 0},
		{value: -0.01},
	} {
		got, ok := paymentAmountToCents(test.value)
		require.Equal(t, test.ok, ok, "amount %v", test.value)
		if test.ok {
			require.Equal(t, test.want, got, "amount %v", test.value)
		}
	}
}

func auditDetail(t *testing.T, audits []*dbent.PaymentAuditLog, action string) string {
	t.Helper()
	for _, audit := range audits {
		if audit.Action == action {
			return audit.Detail
		}
	}
	t.Fatalf("audit action %q was not recorded", action)
	return ""
}
