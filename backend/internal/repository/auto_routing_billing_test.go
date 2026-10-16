package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutingSettlementValidatesImmutableRouteIdentity(t *testing.T) {
	for _, test := range []struct {
		name          string
		executionRows int64
		attemptRows   int64
		executionErr  error
		attemptErr    error
		wantErr       bool
	}{
		{name: "matching immutable execution and account", executionRows: 1, attemptRows: 1},
		{name: "wrong execution or group", wantErr: true},
		{name: "wrong immutable account", executionRows: 1, wantErr: true},
		{name: "execution storage failure", executionErr: errors.New("unavailable"), wantErr: true},
		{name: "attempt storage failure", executionRows: 1, attemptErr: errors.New("unavailable"), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = database.Close() }()
			mock.ExpectBegin()
			tx, err := database.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			groupID := int64(12)
			command := &service.UsageBillingCommand{AutoExecutionID: 7, AutoAttemptNo: 1, APIKeyID: 4, ActualGroupID: &groupID, AccountID: 9}
			expect := mock.ExpectExec("UPDATE auto_route_executions").WithArgs(int64(7), int64(4), groupID)
			if test.executionErr != nil {
				expect.WillReturnError(test.executionErr)
			} else {
				expect.WillReturnResult(sqlmock.NewResult(0, test.executionRows))
			}
			if test.executionRows == 1 && test.executionErr == nil {
				attemptUpdate := mock.ExpectExec("UPDATE auto_route_attempts\\s+SET updated_at = NOW\\(\\)\\s+WHERE account_id = \\$1 AND execution_id = \\$2 AND attempt_no = \\$3 AND group_id = \\$4").
					WithArgs(int64(9), int64(7), 1, groupID)
				if test.attemptErr != nil {
					attemptUpdate.WillReturnError(test.attemptErr)
				} else {
					attemptUpdate.WillReturnResult(sqlmock.NewResult(0, test.attemptRows))
				}
			}
			err = settleAutoRoutingExecution(context.Background(), tx, command)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			mock.ExpectRollback()
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFixedKeySettlementNeedsNoAutoStorage(t *testing.T) {
	require.NoError(t, settleAutoRoutingExecution(context.Background(), nil, &service.UsageBillingCommand{}))
}
