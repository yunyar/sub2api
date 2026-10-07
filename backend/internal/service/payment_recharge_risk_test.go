//go:build unit

package service

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newPaymentRiskTestService(t *testing.T) (*PaymentRiskService, *authCacheInvalidatorStub) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:risk_"+uuid.NewString()+"?mode=memory&cache=shared")
	require.NoError(t, err)
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, database))))
	t.Cleanup(func() { _ = client.Close() })
	migration, err := migrations.FS.ReadFile("244_payment_risk_ip_policy.sql")
	require.NoError(t, err)
	_, err = client.ExecContext(context.Background(), string(migration))
	require.NoError(t, err)
	invalidator := &authCacheInvalidatorStub{}
	service := NewPaymentRiskService(client, invalidator)
	service.rechargeLimit = 10
	service.now = func() time.Time { return time.Unix(1800000000, 0) }
	return service, invalidator
}

func paymentRiskTestUser(t *testing.T, service *PaymentRiskService) *dbent.User {
	t.Helper()
	account, err := service.client.User.Create().
		SetEmail(uuid.NewString() + "@example.com").SetPasswordHash("test-hash").SetBalance(12.34).Save(context.Background())
	require.NoError(t, err)
	return account
}

func paymentRiskTestOrder(t *testing.T, service *PaymentRiskService, account *dbent.User, address string) *dbent.PaymentOrder {
	t.Helper()
	order, err := service.client.PaymentOrder.Create().
		SetUserID(account.ID).SetUserEmail(account.Email).SetUserName("").
		SetAmount(100).SetPayAmount(10).SetFeeRate(0).SetRechargeCode("").
		SetOutTradeNo("risk_" + uuid.NewString()).SetPaymentType("wxpay").SetPaymentTradeNo("").
		SetOrderType("balance").SetStatus(OrderStatusPending).SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP(address).SetSrcHost("example.com").Save(context.Background())
	require.NoError(t, err)
	return order
}

func TestPaymentRiskPrivateIPsNeverRecordedOrBlocked(t *testing.T) {
	service, invalidator := newPaymentRiskTestService(t)
	account := paymentRiskTestUser(t, service)
	ctx := context.Background()
	for _, address := range []string{"", "127.0.0.1", "172.21.0.1", "192.168.1.1", "10.0.0.1", "::1", "fd00::1", "100.64.1.1"} {
		require.NoError(t, service.RecordUserIP(ctx, account.ID, address, "payment"))
		require.NoError(t, service.CheckRecharge(ctx, account.ID, address))
		require.NoError(t, service.CheckIP(ctx, address))
		order := paymentRiskTestOrder(t, service, account, address)
		require.Error(t, service.BlockOrderIP(ctx, order.ID, "confirmed by administrator", "admin:1"))
	}
	rows, err := service.client.QueryContext(ctx, "SELECT COUNT(*) FROM payment_risk_user_ips")
	require.NoError(t, err)
	require.True(t, rows.Next())
	var count int
	require.NoError(t, rows.Scan(&count))
	require.NoError(t, rows.Close())
	require.Zero(t, count)
	require.Empty(t, invalidator.userIDs)
}

func TestPaymentRiskPermanentIPPolicyAndLinkedAccounts(t *testing.T) {
	service, invalidator := newPaymentRiskTestService(t)
	ctx := context.Background()
	owner := paymentRiskTestUser(t, service)
	linked := paymentRiskTestUser(t, service)
	unrelated := paymentRiskTestUser(t, service)
	admin := paymentRiskTestUser(t, service)
	require.NoError(t, service.client.User.UpdateOneID(admin.ID).SetRole(RoleAdmin).Exec(ctx))
	order := paymentRiskTestOrder(t, service, owner, "8.8.8.8")
	require.NoError(t, service.RecordUserIP(ctx, linked.ID, "::ffff:8.8.8.8", "login"))
	require.NoError(t, service.RecordUserIP(ctx, unrelated.ID, "1.1.1.1", "registration"))
	require.NoError(t, service.RecordUserIP(ctx, admin.ID, "8.8.8.8", "login"))
	require.NoError(t, service.BlockOrderIP(ctx, order.ID, "operator-confirmed incident", "admin:1"))
	require.ErrorIs(t, service.CheckIP(ctx, "8.8.8.8"), ErrPaymentRiskIPBlocked)
	require.NoError(t, service.CheckIP(ctx, "1.1.1.1"))
	for _, accountID := range []int64{owner.ID, linked.ID} {
		account, err := service.client.User.Get(ctx, accountID)
		require.NoError(t, err)
		require.Equal(t, StatusDisabled, account.Status)
		require.Equal(t, 12.34, account.Balance)
		require.Contains(t, invalidator.userIDs, accountID)
	}
	for _, accountID := range []int64{unrelated.ID, admin.ID} {
		account, err := service.client.User.Get(ctx, accountID)
		require.NoError(t, err)
		require.Equal(t, StatusActive, account.Status)
	}
	restarted := NewPaymentRiskService(service.client, invalidator)
	require.ErrorIs(t, restarted.CheckIP(ctx, "8.8.8.8"), ErrPaymentRiskIPBlocked)
	records, total, err := restarted.ListBlockedIPs(ctx, 1, 20)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, records, 1)
	require.True(t, records[0].Active)
	require.Equal(t, 3, records[0].LinkedUsers)
	require.Contains(t, records[0].Evidence, order.OutTradeNo)
	require.NoError(t, restarted.UnblockIP(ctx, "8.8.8.8", "admin:1", "reviewed appeal"))
	require.NoError(t, restarted.CheckIP(ctx, "8.8.8.8"))
	stillDisabled, err := service.client.User.Get(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, stillDisabled.Status)
}

func TestPaymentRiskBlocksNewAssociationsAndPeriodicScan(t *testing.T) {
	service, _ := newPaymentRiskTestService(t)
	ctx := context.Background()
	owner := paymentRiskTestUser(t, service)
	order := paymentRiskTestOrder(t, service, owner, "8.8.4.4")
	require.NoError(t, service.BlockOrderIP(ctx, order.ID, "confirmed", "admin:1"))
	newAccount := paymentRiskTestUser(t, service)
	require.ErrorIs(t, service.RecordUserIP(ctx, newAccount.ID, "8.8.4.4", "login"), ErrPaymentRiskIPBlocked)
	account, err := service.client.User.Get(ctx, newAccount.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, account.Status)
	historical := paymentRiskTestUser(t, service)
	paymentRiskTestOrder(t, service, historical, "8.8.4.4")
	count, err := service.ScanBlockedAccounts(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = service.ScanBlockedAccounts(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestPaymentRiskTenMinuteRateBoundaryDoesNotPermanentlyBlockSharedIP(t *testing.T) {
	service, invalidator := newPaymentRiskTestService(t)
	ctx := context.Background()
	first := paymentRiskTestUser(t, service)
	second := paymentRiskTestUser(t, service)
	for attempt := 0; attempt < 10; attempt++ {
		require.NoError(t, service.CheckRecharge(ctx, first.ID, "1.1.1.1"))
	}
	require.Error(t, service.CheckRecharge(ctx, second.ID, "1.1.1.1"))
	firstAfter, err := service.client.User.Get(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, firstAfter.Status)
	secondAfter, err := service.client.User.Get(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, secondAfter.Status)
	require.Equal(t, []int64{second.ID}, invalidator.userIDs)
	require.NoError(t, service.CheckIP(ctx, "1.1.1.1"))
	service.now = func() time.Time { return time.Unix(1800000600, 0) }
	require.NoError(t, service.CheckRecharge(ctx, first.ID, "1.1.1.1"))
}

func TestPaymentRiskStorageFailureFailsClosedForPublicAddresses(t *testing.T) {
	service, _ := newPaymentRiskTestService(t)
	ctx := context.Background()
	account := paymentRiskTestUser(t, service)
	_, err := service.client.ExecContext(ctx, "DROP TABLE payment_risk_ips")
	require.NoError(t, err)
	require.Error(t, service.CheckIP(ctx, "1.1.1.1"))
	require.Error(t, service.CheckRecharge(ctx, account.ID, "1.1.1.1"))
	after, err := service.client.User.Get(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, after.Status)
}

func TestPaymentRiskConcurrentRechargeLimitCannotBeExceeded(t *testing.T) {
	service, _ := newPaymentRiskTestService(t)
	service.invalidator = nil
	account := paymentRiskTestUser(t, service)
	ctx := context.Background()
	var accepted atomic.Int64
	var workers sync.WaitGroup
	start := make(chan struct{})
	for attempt := 0; attempt < 20; attempt++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if service.CheckRecharge(ctx, account.ID, "1.1.1.1") == nil {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	require.Equal(t, int64(10), accepted.Load())
	require.NoError(t, service.CheckIP(ctx, "1.1.1.1"))
}

func TestPaymentRiskAutomaticPolicyRequiresTrustedHistoricalAttribution(t *testing.T) {
	service, _ := newPaymentRiskTestService(t)
	ctx := context.Background()
	account := paymentRiskTestUser(t, service)
	order := paymentRiskTestOrder(t, service, account, "8.8.8.8")
	require.Error(t, service.BlockOrderIP(ctx, order.ID, "amount_mismatch", "gateway-confirmation"))
	require.NoError(t, service.CheckIP(ctx, "8.8.8.8"))
	service.now = func() time.Time { return order.CreatedAt.Add(-time.Second) }
	require.NoError(t, service.RecordUserIP(ctx, account.ID, "8.8.8.8", "payment"))
	require.NoError(t, service.BlockOrderIP(ctx, order.ID, "amount_mismatch", "gateway-confirmation"))
	require.ErrorIs(t, service.CheckIP(ctx, "8.8.8.8"), ErrPaymentRiskIPBlocked)
}

func TestPaymentRiskFailedCheckoutsDoNotAccumulateAccountPenalties(t *testing.T) {
	service, invalidator := newPaymentRiskTestService(t)
	ctx := context.Background()
	account := paymentRiskTestUser(t, service)
	for attempt := 0; attempt < 20; attempt++ {
		reservation, err := service.ReserveRecharge(ctx, account.ID, "1.1.1.1")
		require.NoError(t, err)
		require.NoError(t, service.FinishRecharge(ctx, reservation, false))
	}
	after, err := service.client.User.Get(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, after.Status)
	require.Empty(t, invalidator.userIDs)
}

func TestPaymentRiskInFlightCheckoutsThrottleWithoutAccountBan(t *testing.T) {
	service, invalidator := newPaymentRiskTestService(t)
	ctx := context.Background()
	account := paymentRiskTestUser(t, service)
	var reservations []string
	for attempt := 0; attempt < 10; attempt++ {
		reservation, err := service.ReserveRecharge(ctx, account.ID, "1.1.1.1")
		require.NoError(t, err)
		reservations = append(reservations, reservation)
	}
	_, err := service.ReserveRecharge(ctx, account.ID, "1.1.1.1")
	require.Error(t, err)
	after, err := service.client.User.Get(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, after.Status)
	require.Empty(t, invalidator.userIDs)
	for _, reservation := range reservations {
		require.NoError(t, service.FinishRecharge(ctx, reservation, false))
	}
	require.NoError(t, service.CheckRecharge(ctx, account.ID, "1.1.1.1"))
}
