package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPlaygroundImageBalanceWithPostgres(t *testing.T) {
	dsn := os.Getenv("PLAYGROUND_BILLING_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PLAYGROUND_BILLING_POSTGRES_DSN to test real PostgreSQL transactions")
	}
	root, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	schema := fmt.Sprintf("playground_billing_test_%d", time.Now().UnixNano())
	_, err = root.ExecContext(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = root.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	connectionURL, err := url.Parse(dsn)
	require.NoError(t, err)
	query := connectionURL.Query()
	query.Set("search_path", schema)
	connectionURL.RawQuery = query.Encode()
	database, err := sql.Open("postgres", connectionURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.ExecContext(t.Context(), `
		CREATE TABLE users (id BIGINT PRIMARY KEY, balance NUMERIC(20,8) NOT NULL, frozen_balance NUMERIC(20,8) DEFAULT 0, deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ);
		CREATE TABLE usage_billing_dedup (id BIGSERIAL PRIMARY KEY, request_id TEXT, api_key_id BIGINT, request_fingerprint TEXT, UNIQUE(request_id, api_key_id));
		CREATE TABLE usage_billing_dedup_archive (LIKE usage_billing_dedup INCLUDING ALL);
	`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/240_playground_image_balance_holds.sql")
	require.NoError(t, err)
	_, err = database.ExecContext(t.Context(), string(migration))
	require.NoError(t, err)
	repo := &usageBillingRepository{db: database}
	newHold := func(userID int64, suffix string, amount float64) *service.BatchImageBalanceHoldCommand {
		batchID := fmt.Sprintf("playground-image:%d:%s", userID, suffix)
		return &service.BatchImageBalanceHoldCommand{BatchID: batchID, RequestID: service.BatchImageHoldRequestID(batchID), UserID: userID, APIKeyID: userID, HoldAmount: amount, UnitAmount: amount, RequestedCount: 1}
	}
	settle := func(hold *service.BatchImageBalanceHoldCommand, amount float64) (*service.UsageBillingApplyResult, error) {
		capture := *hold
		capture.RequestID = service.PlaygroundImageTerminalRequestID(hold.BatchID)
		capture.RequestFingerprint = ""
		capture.ActualAmount = amount
		return repo.SettlePlaygroundImageBalance(t.Context(), &capture, &service.UsageBillingCommand{RequestID: "usage:" + hold.BatchID, APIKeyID: hold.APIKeyID, UserID: hold.UserID})
	}
	release := func(hold *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
		refund := *hold
		refund.RequestID = service.PlaygroundImageTerminalRequestID(hold.BatchID)
		refund.RequestFingerprint = ""
		return repo.ReleaseBatchImageBalance(t.Context(), &refund)
	}
	assertBalance := func(userID int64, expectedBalance, expectedFrozen float64) {
		var balance, frozen float64
		require.NoError(t, database.QueryRowContext(t.Context(), "SELECT balance, frozen_balance FROM users WHERE id=$1", userID).Scan(&balance, &frozen))
		require.InDelta(t, expectedBalance, balance, 1e-8)
		require.InDelta(t, expectedFrozen, frozen, 1e-8)
	}
	t.Run("concurrent requests cannot reserve more than the balance", func(t *testing.T) {
		_, err := database.ExecContext(t.Context(), "INSERT INTO users(id,balance) VALUES(1,5)")
		require.NoError(t, err)
		type outcome struct {
			hold *service.BatchImageBalanceHoldCommand
			err  error
		}
		outcomes := make(chan outcome, 8)
		for index := range 8 {
			go func() {
				hold := newHold(1, fmt.Sprint(index), 3)
				_, err := repo.ReserveBatchImageBalance(t.Context(), hold)
				outcomes <- outcome{hold: hold, err: err}
			}()
		}
		var winner *service.BatchImageBalanceHoldCommand
		for range 8 {
			result := <-outcomes
			if result.err == nil {
				require.Nil(t, winner)
				winner = result.hold
			} else {
				require.ErrorIs(t, result.err, service.ErrBatchImageInsufficientBalance)
			}
		}
		require.NotNil(t, winner)
		assertBalance(1, 2, 3)
		captured, err := settle(winner, 1)
		require.NoError(t, err)
		require.True(t, captured.Applied)
		assertBalance(1, 4, 0)
		duplicate, err := settle(winner, 1)
		require.NoError(t, err)
		require.False(t, duplicate.Applied)
		_, err = release(winner)
		require.True(t, err == nil || errors.Is(err, service.ErrUsageBillingRequestConflict))
		assertBalance(1, 4, 0)
	})
	t.Run("capture and refund cannot consume another outstanding hold", func(t *testing.T) {
		for userID := int64(10); userID < 20; userID++ {
			_, err := database.ExecContext(t.Context(), "INSERT INTO users(id,balance) VALUES($1,20)", userID)
			require.NoError(t, err)
			hold := newHold(userID, "racing", 4)
			_, err = repo.ReserveBatchImageBalance(t.Context(), hold)
			require.NoError(t, err)
			_, err = repo.ReserveBatchImageBalance(t.Context(), newHold(userID, "other", 8))
			require.NoError(t, err)
			var wait sync.WaitGroup
			wait.Add(2)
			go func() { defer wait.Done(); _, _ = settle(hold, 2) }()
			go func() { defer wait.Done(); _, _ = release(hold) }()
			wait.Wait()
			var status string
			require.NoError(t, database.QueryRowContext(t.Context(), "SELECT status FROM playground_image_balance_holds WHERE batch_id=$1", hold.BatchID).Scan(&status))
			switch status {
			case "captured":
				assertBalance(userID, 10, 8)
			case "released":
				assertBalance(userID, 12, 8)
			default:
				t.Fatalf("unsettled hold: %s", status)
			}
		}
	})
	t.Run("settlement above hold leaves the reservation intact", func(t *testing.T) {
		_, err := database.ExecContext(t.Context(), "INSERT INTO users(id,balance) VALUES(20,5)")
		require.NoError(t, err)
		hold := newHold(20, "above-hold", 2)
		_, err = repo.ReserveBatchImageBalance(t.Context(), hold)
		require.NoError(t, err)
		assertBalance(20, 3, 2)

		_, err = settle(hold, 3)
		require.ErrorIs(t, err, service.ErrBatchImageSettlementCostExceedsHold)
		assertBalance(20, 3, 2)

		_, err = release(hold)
		require.NoError(t, err)
		assertBalance(20, 5, 0)
	})
}
