package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutingFinalizationWithPostgres(t *testing.T) {
	dsn := os.Getenv("PLAYGROUND_BILLING_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PLAYGROUND_BILLING_POSTGRES_DSN to test real PostgreSQL transactions")
	}
	root, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	schema := fmt.Sprintf("auto_finalization_test_%d", time.Now().UnixNano())
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
		CREATE TABLE auto_route_executions (
			id BIGSERIAL PRIMARY KEY, api_key_id BIGINT NOT NULL, actual_group_id BIGINT NOT NULL,
			status TEXT NOT NULL DEFAULT 'dispatching', current_attempt INTEGER NOT NULL DEFAULT 1,
			candidate_group_ids JSONB NOT NULL DEFAULT '[12,13]',
			settlement_status TEXT NOT NULL DEFAULT 'pending', result_reference TEXT, evidence_code TEXT,
			completed_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE auto_route_attempts (
			execution_id BIGINT NOT NULL, attempt_no INTEGER NOT NULL, group_id BIGINT NOT NULL,
			account_id BIGINT NOT NULL, status TEXT NOT NULL DEFAULT 'dispatching', result_reference TEXT,
			completed_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (execution_id, attempt_no)
		);
	`)
	require.NoError(t, err)
	repo := &autoRoutingRepository{db: database}
	newExecution := func(t *testing.T) int64 {
		t.Helper()
		var executionID int64
		require.NoError(t, database.QueryRowContext(t.Context(), `
			INSERT INTO auto_route_executions (api_key_id, actual_group_id)
			VALUES (4, 12) RETURNING id
		`).Scan(&executionID))
		_, err := database.ExecContext(t.Context(), `
			INSERT INTO auto_route_attempts (execution_id, attempt_no, group_id, account_id)
			VALUES ($1, 1, 12, 9)
		`, executionID)
		require.NoError(t, err)
		return executionID
	}
	settle := func(ctx context.Context, executionID int64) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		groupID := int64(12)
		if err := settleAutoRoutingExecution(ctx, tx, &service.UsageBillingCommand{
			AutoExecutionID: executionID, AutoAttemptNo: 1, APIKeyID: 4,
			ActualGroupID: &groupID, AccountID: 9,
		}); err != nil {
			return err
		}
		return tx.Commit()
	}
	assertState := func(t *testing.T, executionID int64, expectedStatus, expectedSettlement string) {
		t.Helper()
		var status, settlement, attemptStatus string
		var completed, attemptCompleted sql.NullTime
		require.NoError(t, database.QueryRowContext(t.Context(), `
			SELECT execution.status, execution.settlement_status, execution.completed_at,
			       attempt.status, attempt.completed_at
			FROM auto_route_executions execution
			JOIN auto_route_attempts attempt ON attempt.execution_id = execution.id
			WHERE execution.id = $1 AND attempt.attempt_no = 1
		`, executionID).Scan(&status, &settlement, &completed, &attemptStatus, &attemptCompleted))
		require.Equal(t, expectedStatus, status)
		require.Equal(t, expectedStatus, attemptStatus)
		require.Equal(t, expectedSettlement, settlement)
		require.Equal(t, expectedStatus != "dispatching", completed.Valid)
		require.Equal(t, expectedStatus != "dispatching", attemptCompleted.Valid)
	}
	for _, test := range []struct {
		name        string
		settleFirst bool
		unknown     bool
	}{
		{name: "completion before settlement"},
		{name: "settlement before completion", settleFirst: true},
		{name: "unknown before settlement", unknown: true},
		{name: "settlement before unknown", settleFirst: true, unknown: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			executionID := newExecution(t)
			if test.settleFirst {
				require.NoError(t, settle(t.Context(), executionID))
			}
			var changed bool
			var err error
			expectedStatus := "completed"
			if test.unknown {
				expectedStatus = "unknown"
				changed, err = repo.MarkAutoRoutingAttemptUnknown(t.Context(), executionID, 1, "stream_interrupted")
			} else {
				changed, err = repo.CompleteAutoRoutingAttempt(t.Context(), executionID, 1, "verified-response")
			}
			require.NoError(t, err)
			require.True(t, changed)
			if !test.settleFirst {
				require.NoError(t, settle(t.Context(), executionID))
			}
			assertState(t, executionID, expectedStatus, "settled")
			changed, err = repo.CompleteAutoRoutingAttempt(t.Context(), executionID, 1, "duplicate-response")
			require.NoError(t, err)
			require.False(t, changed)
			changed, err = repo.MarkAutoRoutingAttemptUnknown(t.Context(), executionID, 1, "late_timeout")
			require.NoError(t, err)
			require.False(t, changed)
			assertState(t, executionID, expectedStatus, "settled")
		})
	}
	t.Run("settled execution rejects another attempt", func(t *testing.T) {
		executionID := newExecution(t)
		require.NoError(t, settle(t.Context(), executionID))
		changed, err := repo.CompleteAutoRoutingAttempt(t.Context(), executionID, 2, "wrong-attempt")
		require.NoError(t, err)
		require.False(t, changed)
		assertState(t, executionID, "dispatching", "settled")
	})
	t.Run("settled execution cannot retry or release its dispatch", func(t *testing.T) {
		executionID := newExecution(t)
		require.NoError(t, settle(t.Context(), executionID))
		prepared, err := repo.PrepareNextAutoRoutingAttempt(
			t.Context(), executionID, 1, 13, service.AutoRouteEvidenceUpstreamFailure,
		)
		require.NoError(t, err)
		require.False(t, prepared)
		require.ErrorIs(t, repo.ReleaseAutoRoutingDispatchClaim(t.Context(), executionID, 12), service.ErrAutoRoutingExecutionStateConflict)
		assertState(t, executionID, "dispatching", "settled")
		changed, err := repo.CompleteAutoRoutingAttempt(t.Context(), executionID, 1, "verified-response")
		require.NoError(t, err)
		require.True(t, changed)
		assertState(t, executionID, "completed", "settled")
	})
	t.Run("settlement and completion may race", func(t *testing.T) {
		for range 12 {
			executionID := newExecution(t)
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() {
				<-start
				results <- settle(t.Context(), executionID)
			}()
			go func() {
				<-start
				changed, err := repo.CompleteAutoRoutingAttempt(t.Context(), executionID, 1, "verified-response")
				if err == nil && !changed {
					err = fmt.Errorf("completion rejected for execution %d", executionID)
				}
				results <- err
			}()
			close(start)
			for range 2 {
				require.NoError(t, <-results)
			}
			assertState(t, executionID, "completed", "settled")
		}
	})
}
