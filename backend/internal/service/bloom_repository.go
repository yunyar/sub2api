package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

type BloomDifficulty string

const (
	BloomDailyEasy   BloomDifficulty = "easy"
	BloomDailyMedium BloomDifficulty = "medium"
	BloomDailyHard   BloomDifficulty = "hard"
	BloomWeekly      BloomDifficulty = "weekly"
)

var (
	ErrBloomInvalidInput   = errors.New("invalid bloom storage input")
	ErrBloomUserNotFound   = errors.New("active bloom user not found")
	ErrBloomTaskNotFound   = errors.New("bloom task not found")
	ErrBloomExpired        = errors.New("bloom task has expired")
	ErrBloomBudgetExceeded = errors.New("bloom weekly budget exceeded")
	ErrBloomBudgetChanged  = errors.New("bloom drafts use a different weekly budget")
)

type BloomTaskDraft struct {
	PeriodStart  time.Time
	Difficulty   BloomDifficulty
	Template     json.RawMessage
	RewardMicros int64
}

type BloomTask struct {
	BloomTaskDraft
	ID        int64
	ClaimedAt *time.Time
}

type BloomWeek struct {
	Exists       bool
	BudgetMicros int64
	SpentMicros  int64
	Tasks        []BloomTask
}

type BloomClaim struct {
	Applied      bool
	RewardMicros int64
	BalanceAfter decimal.Decimal
	ClaimedAt    time.Time
	AlertEventID int64 `json:"-"`
}

type BloomUsageStats struct {
	TotalRecords   int64
	WalletCost     decimal.Decimal
	OutputRequests int64
	OutputTokens   int64
	ImageCount     int64
	ActiveDays     int64
}

// BloomRepository takes the authenticated users.id from the caller. The caller
// checks task progress and claim eligibility before calling ClaimTask.
// Week and period dates use the supplied time's calendar date, with Monday as
// week start. Usage ranges are [start, end) instants; zone is an IANA time zone
// used to count distinct dates with valid output.
type BloomRepository interface {
	GetWeek(ctx context.Context, userID int64, weekStart time.Time) (BloomWeek, error)
	// EnsureTasks with no drafts initializes or reads the authoritative budget.
	// Drafts that use a different budget return the persisted week and ErrBloomBudgetChanged.
	EnsureTasks(ctx context.Context, userID int64, weekStart time.Time, budgetMicros int64, drafts []BloomTaskDraft) (BloomWeek, error)
	ClaimTask(ctx context.Context, userID, taskID int64, expiresAt time.Time) (BloomClaim, error)
	UsageStats(ctx context.Context, userID int64, start, end time.Time, zone string) (BloomUsageStats, error)
	CountClaimedWeekly(ctx context.Context, userID int64) (int64, error)
}
