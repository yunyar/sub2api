package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AutoRoutingModeAuto = "auto"

	AutoRoutingSelectionInherit = "inherit"
	AutoRoutingSelectionCustom  = "custom"

	AutoRoutingProfileStatusActive   = "active"
	AutoRoutingProfileStatusDisabled = "disabled"

	AutoRouteExecutionPrepared    = "prepared"
	AutoRouteExecutionDispatching = "dispatching"
	AutoRouteExecutionCompleted   = "completed"
	AutoRouteExecutionFailed      = "failed"
	AutoRouteExecutionUnknown     = "unknown"

	AutoRouteAttemptPrepared    = "prepared"
	AutoRouteAttemptDispatching = "dispatching"
	AutoRouteAttemptCompleted   = "completed"
	AutoRouteAttemptFailed      = "failed"
	AutoRouteAttemptUnknown     = "unknown"
)

var ErrAutoRoutingRevisionConflict = infraerrors.Conflict(
	"AUTO_ROUTING_REVISION_CONFLICT",
	"auto routing configuration changed; reload and retry",
)

type AutoRoutingProfileMember struct {
	GroupID   int64
	SortOrder int
}

type AutoRoutingProfile struct {
	ID                     int64
	Name                   string
	Status                 string
	Revision               int64
	MaxCandidates          int
	AllowCustomOrder       bool
	CrossGroupRetryAllowed bool
	ModelCatalogFilter     *GroupModelAllowlist
	Members                []AutoRoutingProfileMember
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// A missing APIKeyAutoRoutingConfig row means fixed routing; the key's fixed
// group remains in api_keys.group_id.
type APIKeyAutoRoutingConfig struct {
	APIKeyID        int64
	Mode            string
	ProfileID       int64
	SelectionMode   string
	GroupIDs        []int64
	CrossGroupRetry bool
	// Revision mirrors api_keys.routing_revision and remains monotonic in fixed mode.
	Revision  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AutoRoutingExecutionInput struct {
	APIKeyID          int64
	RequestID         string
	PayloadHash       string
	ProfileID         int64
	ProfileRevision   int64
	RoutingRevision   int64
	CandidateGroupIDs []int64
	SelectedGroupID   int64
}

// The request payload is represented only by PayloadHash; never persist its
// raw bytes here. ActualGroupID is the immutable selected candidate, including
// while prepared, before an upstream dispatch has been claimed.
type AutoRoutingExecution struct {
	ID                int64
	APIKeyID          int64
	RequestID         string
	PayloadHash       string
	ProfileID         int64
	ProfileRevision   int64
	RoutingRevision   int64
	CandidateGroupIDs []int64
	Status            string
	CurrentAttempt    int
	ActualGroupID     *int64
	ResultReference   *string
	EvidenceCode      *string
	SettlementStatus  string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
}

type AutoRoutingAttempt struct {
	ID                 int64
	ExecutionID        int64
	AttemptNo          int
	GroupID            int64
	Status             string
	AccountID          *int64
	Platform           *string
	BillingSnapshotRef *string
	UpstreamRequestID  *string
	UpstreamTaskID     *string
	ResultReference    *string
	DispatchedAt       *time.Time
	CompletedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type PlaygroundAutoKeyBinding struct {
	UserID    int64
	ProfileID int64
	APIKeyID  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type AutoRoutingRepository interface {
	CreateAutoRoutingProfile(ctx context.Context, profile *AutoRoutingProfile) error
	GetAutoRoutingProfile(ctx context.Context, id int64) (*AutoRoutingProfile, error)
	ListAutoRoutingProfiles(ctx context.Context) ([]AutoRoutingProfile, error)
	UpdateAutoRoutingProfile(ctx context.Context, profile *AutoRoutingProfile, expectedRevision int64) error

	// expectedRevision is api_keys.routing_revision in either mode. A nil
	// config writes fixed mode and fixedGroupID; an Auto config requires a
	// profile and atomically clears api_keys.group_id.
	ReplaceAPIKeyAutoRouting(ctx context.Context, apiKeyID int64, config *APIKeyAutoRoutingConfig, fixedGroupID *int64, expectedRevision int64) error
	GetAPIKeyAutoRouting(ctx context.Context, apiKeyID int64) (*APIKeyAutoRoutingConfig, error)

	// Existing request IDs return their immutable record; callers compare
	// PayloadHash and reject mismatched replays before dispatch.
	CreateOrGetAutoRoutingExecution(ctx context.Context, input AutoRoutingExecutionInput) (*AutoRoutingExecution, bool, error)
	GetAutoRoutingExecution(ctx context.Context, apiKeyID int64, requestID string) (*AutoRoutingExecution, error)
	ClaimAutoRoutingAttemptDispatch(ctx context.Context, attempt *AutoRoutingAttempt) (bool, error)
	CompleteAutoRoutingAttempt(ctx context.Context, executionID int64, attemptNo int, resultReference string) (bool, error)
	MarkAutoRoutingAttemptUnknown(ctx context.Context, executionID int64, attemptNo int, evidenceCode string) (bool, error)

	GetPlaygroundAutoKeyBinding(ctx context.Context, userID, profileID int64) (*PlaygroundAutoKeyBinding, error)
	// Upsert inserts only when no binding exists, then replaces the supplied
	// value with the authoritative winner. It does not manage API key lifecycle.
	UpsertPlaygroundAutoKeyBinding(ctx context.Context, binding *PlaygroundAutoKeyBinding) error
}
