//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutingRepository_ProfileUpdateUsesRevisionCAS(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("UPDATE auto_group_profiles").
		WithArgs(int64(17), "Auto", service.AutoRoutingProfileStatusDisabled, 8, true, false, int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"revision", "updated_at"}))
	mock.ExpectRollback()

	repo := &autoRoutingRepository{db: database}
	err = repo.UpdateAutoRoutingProfile(context.Background(), &service.AutoRoutingProfile{
		ID:               17,
		Name:             "Auto",
		Status:           service.AutoRoutingProfileStatusDisabled,
		MaxCandidates:    8,
		AllowCustomOrder: true,
	}, 3)
	require.ErrorIs(t, err, service.ErrAutoRoutingRevisionConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_ReplaceAPIKeyRoutingIsAtomic(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT set_config").
		WillReturnRows(sqlmock.NewRows([]string{"set_config"}).AddRow("on"))
	mock.ExpectQuery("SELECT group_id, routing_mode, routing_revision FROM api_keys").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "routing_mode", "routing_revision"}).
			AddRow(nil, "fixed", int64(0)))
	mock.ExpectQuery("SELECT revision FROM auto_key_routing").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"revision"}))
	mock.ExpectQuery("SELECT status = 'active' FROM auto_group_profiles").
		WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(true))
	mock.ExpectExec("UPDATE api_keys SET group_id = NULL").
		WithArgs(int64(41)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO auto_key_routing").
		WithArgs(int64(41), int64(6), service.AutoRoutingSelectionCustom, false, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM auto_key_routing_groups").
		WithArgs(int64(41)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO auto_key_routing_groups").
		WithArgs(int64(41), int64(9), 0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO auto_key_routing_groups").
		WithArgs(int64(41), int64(12), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE api_keys").
		WithArgs(int64(41), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	config := &service.APIKeyAutoRoutingConfig{
		APIKeyID:      41,
		Mode:          service.AutoRoutingModeAuto,
		ProfileID:     6,
		SelectionMode: service.AutoRoutingSelectionCustom,
		GroupIDs:      []int64{9, 12},
	}
	err = (&autoRoutingRepository{db: database}).ReplaceAPIKeyAutoRouting(
		context.Background(), 41, config, nil, 0,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), config.Revision)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_CreateOrGetPersistsPreparedSelection(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	now := time.Now().UTC()
	input := service.AutoRoutingExecutionInput{
		APIKeyID:          4,
		RequestID:         "request-1",
		PayloadHash:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProfileID:         2,
		ProfileRevision:   6,
		RoutingRevision:   8,
		CandidateGroupIDs: []int64{12, 14},
		SelectedGroupID:   14,
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, api_key_id, request_id, payload_hash").
		WithArgs(int64(4), "request-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "api_key_id", "request_id", "payload_hash", "profile_id", "profile_revision",
			"routing_revision", "candidate_group_ids", "status", "current_attempt", "actual_group_id",
			"result_reference", "evidence_code", "settlement_status", "created_at", "updated_at", "completed_at",
		}))
	mock.ExpectQuery("SELECT group_id, routing_mode, routing_revision, deleted_at FROM api_keys").
		WithArgs(int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{
			"group_id", "routing_mode", "routing_revision", "deleted_at",
		}).AddRow(nil, service.AutoRoutingModeAuto, int64(8), nil))
	mock.ExpectQuery("SELECT routing.profile_id, routing.revision").
		WithArgs(int64(4)).
		WillReturnRows(sqlmock.NewRows([]string{"profile_id", "routing_revision", "profile_revision", "status"}).
			AddRow(int64(2), int64(8), int64(6), service.AutoRoutingProfileStatusActive))
	mock.ExpectQuery("INSERT INTO auto_route_executions").
		WithArgs(int64(4), "request-1", input.PayloadHash, int64(2), int64(6), int64(8), "[12,14]", int64(14)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "api_key_id", "request_id", "payload_hash", "profile_id", "profile_revision",
			"routing_revision", "candidate_group_ids", "status", "current_attempt", "actual_group_id",
			"result_reference", "evidence_code", "settlement_status", "created_at", "updated_at", "completed_at",
		}).AddRow(int64(22), int64(4), "request-1", input.PayloadHash, int64(2), int64(6), int64(8),
			[]byte(`[12,14]`), service.AutoRouteExecutionPrepared, 0, int64(14), nil, nil, "pending", now, now, nil))
	mock.ExpectCommit()

	execution, created, err := (&autoRoutingRepository{db: database}).CreateOrGetAutoRoutingExecution(context.Background(), input)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, execution.ActualGroupID)
	require.Equal(t, int64(14), *execution.ActualGroupID)
	require.Equal(t, []int64{12, 14}, execution.CandidateGroupIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_CreateOrGetReturnsExistingPayloadForGuardValidation(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	now := time.Now().UTC()
	input := service.AutoRoutingExecutionInput{
		APIKeyID:          4,
		RequestID:         "request-1",
		PayloadHash:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProfileID:         2,
		ProfileRevision:   6,
		RoutingRevision:   8,
		CandidateGroupIDs: []int64{12},
		SelectedGroupID:   12,
	}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, api_key_id, request_id, payload_hash").
		WithArgs(int64(4), "request-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "api_key_id", "request_id", "payload_hash", "profile_id", "profile_revision",
			"routing_revision", "candidate_group_ids", "status", "current_attempt", "actual_group_id",
			"result_reference", "evidence_code", "settlement_status", "created_at", "updated_at", "completed_at",
		}).AddRow(int64(22), int64(4), "request-1",
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			int64(2), int64(6), int64(8), []byte(`[12]`), service.AutoRouteExecutionPrepared,
			0, int64(12), nil, nil, "pending", now, now, nil))
	mock.ExpectCommit()

	execution, created, err := (&autoRoutingRepository{db: database}).CreateOrGetAutoRoutingExecution(context.Background(), input)
	require.NoError(t, err)
	require.False(t, created)
	require.NotNil(t, execution)
	require.NotEqual(t, input.PayloadHash, execution.PayloadHash)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_ReplaceFixedUsesMonotonicKeyRevision(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT set_config").
		WillReturnRows(sqlmock.NewRows([]string{"set_config"}).AddRow("on"))
	mock.ExpectQuery("SELECT group_id, routing_mode, routing_revision FROM api_keys").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "routing_mode", "routing_revision"}).
			AddRow(nil, service.AutoRoutingModeAuto, int64(7)))
	mock.ExpectQuery("SELECT revision FROM auto_key_routing").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(int64(7)))
	mock.ExpectExec("DELETE FROM auto_key_routing WHERE api_key_id").
		WithArgs(int64(41)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE api_keys").
		WithArgs(int64(41), int64(16), int64(8)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	fixedGroupID := int64(16)
	err = (&autoRoutingRepository{db: database}).ReplaceAPIKeyAutoRouting(
		context.Background(), 41, nil, &fixedGroupID, 7,
	)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_FixedRevisionCASDoesNotResetAfterAutoRemoval(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT set_config").
		WillReturnRows(sqlmock.NewRows([]string{"set_config"}).AddRow("on"))
	mock.ExpectQuery("SELECT group_id, routing_mode, routing_revision FROM api_keys").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "routing_mode", "routing_revision"}).
			AddRow(nil, "fixed", int64(9)))
	mock.ExpectQuery("SELECT revision FROM auto_key_routing").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"revision"}))
	mock.ExpectRollback()

	err = (&autoRoutingRepository{db: database}).ReplaceAPIKeyAutoRouting(
		context.Background(), 41, nil, nil, 0,
	)
	require.ErrorIs(t, err, service.ErrAutoRoutingRevisionConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_GetAPIKeyAutoRoutingAbsenceMeansFixed(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT group_id, routing_mode, routing_revision, deleted_at FROM api_keys").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "routing_mode", "routing_revision", "deleted_at"}).
			AddRow(nil, "fixed", int64(2), nil))
	mock.ExpectQuery("SELECT api_key_id, routing_mode, profile_id").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "routing_mode", "profile_id", "selection_mode", "cross_group_retry", "revision", "created_at", "updated_at"}))
	mock.ExpectCommit()

	config, err := (&autoRoutingRepository{db: database}).GetAPIKeyAutoRouting(context.Background(), 41)
	require.NoError(t, err)
	require.Nil(t, config)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_GetAPIKeyAutoRoutingRejectsFixedGroupInvariant(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT group_id, routing_mode, routing_revision, deleted_at FROM api_keys").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "routing_mode", "routing_revision", "deleted_at"}).
			AddRow(int64(7), service.AutoRoutingModeAuto, int64(1), nil))
	mock.ExpectQuery("SELECT api_key_id, routing_mode, profile_id").
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{
			"api_key_id", "routing_mode", "profile_id", "selection_mode", "cross_group_retry",
			"revision", "created_at", "updated_at",
		}).AddRow(int64(41), service.AutoRoutingModeAuto, int64(2), service.AutoRoutingSelectionInherit, false, int64(1), time.Now(), time.Now()))
	mock.ExpectRollback()

	config, err := (&autoRoutingRepository{db: database}).GetAPIKeyAutoRouting(context.Background(), 41)
	require.Nil(t, config)
	require.ErrorContains(t, err, "invalid Auto API key routing invariant")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_ClaimBindsPersistedSelectedGroupAtomically(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()
	now := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, current_attempt, actual_group_id, candidate_group_ids").
		WithArgs(int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "current_attempt", "actual_group_id", "candidate_group_ids"}).
			AddRow(service.AutoRouteExecutionPrepared, 0, int64(14), []byte(`[12,14]`)))
	mock.ExpectQuery("SELECT group_id FROM auto_route_attempts").
		WithArgs(int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
	mock.ExpectQuery("INSERT INTO auto_route_attempts").
		WithArgs(int64(22), int64(14), nil, nil, nil, nil, nil).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "dispatched_at", "created_at", "updated_at"}).
			AddRow(int64(30), service.AutoRouteAttemptDispatching, now, now, now))
	mock.ExpectExec("UPDATE auto_route_executions").
		WithArgs(int64(22), int64(14)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	attempt := &service.AutoRoutingAttempt{ExecutionID: 22, AttemptNo: 1, GroupID: 14}
	claimed, err := (&autoRoutingRepository{db: database}).ClaimAutoRoutingAttemptDispatch(context.Background(), attempt)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, int64(30), attempt.ID)
	require.Equal(t, service.AutoRouteAttemptDispatching, attempt.Status)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_ClaimRejectsDifferentPreparedSelection(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status, current_attempt, actual_group_id, candidate_group_ids").
		WithArgs(int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "current_attempt", "actual_group_id", "candidate_group_ids"}).
			AddRow(service.AutoRouteExecutionPrepared, 0, int64(14), []byte(`[12,14]`)))
	mock.ExpectRollback()

	claimed, err := (&autoRoutingRepository{db: database}).ClaimAutoRoutingAttemptDispatch(context.Background(), &service.AutoRoutingAttempt{
		ExecutionID: 22,
		AttemptNo:   1,
		GroupID:     12,
	})
	require.ErrorContains(t, err, "does not match persisted")
	require.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_PlaygroundBindingInsertReturnsWinner(t *testing.T) {
	for _, test := range []struct {
		name         string
		insertedRows int64
		winnerKeyID  int64
		hasWinner    bool
	}{
		{name: "new binding", insertedRows: 1, winnerKeyID: 51, hasWinner: true},
		{name: "existing binding wins", insertedRows: 0, winnerKeyID: 49, hasWinner: true},
		{name: "key already bound elsewhere", insertedRows: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			database, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = database.Close() }()

			now := time.Now().UTC()
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT key.user_id, routing.profile_id").
				WithArgs(int64(51)).
				WillReturnRows(sqlmock.NewRows([]string{"user_id", "profile_id"}).AddRow(int64(8), int64(6)))
			mock.ExpectExec("INSERT INTO playground_auto_key_bindings \\(user_id, profile_id, api_key_id\\)\\s+VALUES \\(\\$1, \\$2, \\$3\\)\\s+ON CONFLICT DO NOTHING").
				WithArgs(int64(8), int64(6), int64(51)).
				WillReturnResult(sqlmock.NewResult(0, test.insertedRows))
			winnerQuery := mock.ExpectQuery("SELECT user_id, profile_id, api_key_id, created_at, updated_at").
				WithArgs(int64(8), int64(6))
			if test.hasWinner {
				winnerQuery.WillReturnRows(sqlmock.NewRows([]string{"user_id", "profile_id", "api_key_id", "created_at", "updated_at"}).
					AddRow(int64(8), int64(6), test.winnerKeyID, now, now))
				mock.ExpectCommit()
			} else {
				winnerQuery.WillReturnRows(sqlmock.NewRows([]string{"user_id", "profile_id", "api_key_id", "created_at", "updated_at"}))
				mock.ExpectRollback()
			}

			binding := &service.PlaygroundAutoKeyBinding{UserID: 8, ProfileID: 6, APIKeyID: 51}
			err = (&autoRoutingRepository{db: database}).UpsertPlaygroundAutoKeyBinding(context.Background(), binding)
			if test.hasWinner {
				require.NoError(t, err)
				require.Equal(t, test.winnerKeyID, binding.APIKeyID)
				require.Equal(t, now, binding.CreatedAt)
			} else {
				require.ErrorContains(t, err, "already bound to another profile")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAutoRoutingRepository_CompleteAttemptIsAtomicAndPreservesSettlement(t *testing.T) {
	database, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = database.Close() }()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE auto_route_attempts").
		WithArgs(int64(22), 1, service.AutoRouteAttemptCompleted, "result-ref").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE auto_route_executions SET status = \\$2, result_reference = NULLIF\\(\\$3, ''\\), evidence_code = NULLIF\\(\\$4, ''\\), completed_at = NOW\\(\\), updated_at = NOW\\(\\)").
		WithArgs(int64(22), service.AutoRouteExecutionCompleted, "result-ref", "", 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	completed, err := (&autoRoutingRepository{db: database}).CompleteAutoRoutingAttempt(context.Background(), 22, 1, "result-ref")
	require.NoError(t, err)
	require.True(t, completed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAutoRoutingRepository_CreateExecutionRejectsInvalidSelection(t *testing.T) {
	repo := &autoRoutingRepository{db: nil}
	input := service.AutoRoutingExecutionInput{
		APIKeyID:          4,
		RequestID:         "request-1",
		PayloadHash:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProfileID:         2,
		ProfileRevision:   1,
		RoutingRevision:   1,
		CandidateGroupIDs: []int64{12},
		SelectedGroupID:   14,
	}
	execution, created, err := repo.CreateOrGetAutoRoutingExecution(context.Background(), input)
	require.Nil(t, execution)
	require.False(t, created)
	require.ErrorContains(t, err, "not in Auto routing candidate")
}
