package repository

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestListBloomRewardHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	createdAt := time.Date(2026, 11, 24, 14, 30, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*)")).
		WithArgs("alice@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT -task.id")).
		WithArgs("alice@example.com", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "email", "username", "value", "created_at", "difficulty",
		}).
			AddRow(-8, 12, "alice@example.com", "Alice", 0.000001, createdAt, "easy").
			AddRow(-7, 12, "alice@example.com", "Alice", 0, createdAt.Add(-time.Hour), "weekly"))

	repo := &redeemCodeRepository{db: db}
	codes, result, err := repo.ListBloomRewardHistory(
		t.Context(),
		pagination.PaginationParams{Page: 1, PageSize: 20},
		"alice@example.com",
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Total)
	require.Len(t, codes, 2)
	require.Equal(t, "bloom_reward", codes[0].Type)
	require.Equal(t, "bloom", codes[0].ActivitySource)
	require.Equal(t, "easy", codes[0].ActivityDescription)
	require.InDelta(t, 0.000001, codes[0].Value, 1e-12)
	require.EqualValues(t, 12, *codes[0].UsedBy)
	require.Equal(t, "alice@example.com", codes[0].User.Email)
	require.Equal(t, 0.0, codes[1].Value)
	require.Equal(t, "weekly", codes[1].ActivityDescription)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListUserActivityPaginatedIncludesClaimedGardenTasks(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	claimedAt := time.Date(2026, 11, 24, 14, 30, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT activity.id")).
		WithArgs(int64(12), 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "used_at", "reward_micros", "difficulty",
		}).AddRow(-8, claimedAt, 1, "easy"))

	repo := &redeemCodeRepository{db: db}
	codes, result, err := repo.ListUserActivityPaginated(
		t.Context(),
		12,
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	require.Len(t, codes, 1)
	require.Equal(t, "bloom", codes[0].ActivitySource)
	require.InDelta(t, 0.000001, codes[0].Value, 1e-12)
	require.NoError(t, mock.ExpectationsWereMet())
}
