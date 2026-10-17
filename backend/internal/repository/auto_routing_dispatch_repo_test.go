package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutingReleaseUnsentClaimIsAtomic(t *testing.T) {
	for _, test := range []struct {
		name          string
		executionRows int64
		attemptRows   int64
	}{
		{"release unsent attempt", 1, 1},
		{"reject settled or changed execution", 0, 0},
		{"rollback when attempt changed", 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = database.Close() })
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT current_attempt").
				WithArgs(int64(7), int64(3)).
				WillReturnRows(sqlmock.NewRows([]string{"current_attempt"}).AddRow(1))
			mock.ExpectExec("UPDATE auto_route_executions").WithArgs(int64(7), int64(3), int64(1)).WillReturnResult(sqlmock.NewResult(0, test.executionRows))
			if test.executionRows == 1 {
				mock.ExpectExec("DELETE FROM auto_route_attempts").WithArgs(int64(7), int64(3), int64(1)).WillReturnResult(sqlmock.NewResult(0, test.attemptRows))
			}
			success := test.executionRows == 1 && test.attemptRows == 1
			if success {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err = (&autoRoutingRepository{db: database}).ReleaseAutoRoutingDispatchClaim(context.Background(), 7, 3)
			if success {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrAutoRoutingExecutionStateConflict)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
