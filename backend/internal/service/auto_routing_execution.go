package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AutoRouteEvidenceUpstreamTimeout       = "upstream_timeout"
	AutoRouteEvidenceConnectionInterrupted = "connection_interrupted"
	AutoRouteEvidenceProcessRecovery       = "process_recovery"
	AutoRouteEvidenceClientCanceled        = "client_canceled_after_dispatch"
	AutoRouteEvidenceResponseStarted       = "response_started"
	AutoRouteEvidenceUpstreamFailure       = "upstream_failure"
)

var (
	ErrAutoRoutingExecutionInvalid = infraerrors.BadRequest(
		"AUTO_ROUTING_EXECUTION_INVALID",
		"auto routing execution request is invalid",
	)
	ErrAutoRoutingExecutionPayloadConflict = infraerrors.Conflict(
		"AUTO_ROUTING_EXECUTION_PAYLOAD_CONFLICT",
		"request id was already used with a different payload",
	)
	ErrAutoRoutingExecutionInProgress = infraerrors.Conflict(
		"AUTO_ROUTING_EXECUTION_IN_PROGRESS",
		"request execution is already in progress",
	)
	ErrAutoRoutingExecutionUnknown = infraerrors.Conflict(
		"AUTO_ROUTING_EXECUTION_UNKNOWN",
		"request execution outcome is unknown and cannot be retried",
	)
	ErrAutoRoutingExecutionCompleted = infraerrors.Conflict(
		"AUTO_ROUTING_EXECUTION_COMPLETED",
		"request execution has already completed",
	)
	ErrAutoRoutingExecutionFailed = infraerrors.Conflict(
		"AUTO_ROUTING_EXECUTION_FAILED",
		"request execution has already failed",
	)
	ErrAutoRoutingExecutionStateConflict = infraerrors.Conflict(
		"AUTO_ROUTING_EXECUTION_STATE_CONFLICT",
		"request execution state changed; do not retry automatically",
	)
	ErrAutoRoutingExecutionStoreUnavailable = infraerrors.ServiceUnavailable(
		"AUTO_ROUTING_EXECUTION_STORE_UNAVAILABLE",
		"auto routing execution store is unavailable",
	)
)

type AutoRoutingExecutionRequest struct {
	APIKeyID          int64
	RequestID         string
	Body              []byte
	ProfileID         int64
	ProfileRevision   int64
	RoutingRevision   int64
	CandidateGroupIDs []int64
	SelectedGroupID   int64
}

type autoRoutingExecutionRepository interface {
	CreateOrGetAutoRoutingExecution(ctx context.Context, input AutoRoutingExecutionInput) (*AutoRoutingExecution, bool, error)
	GetAutoRoutingExecution(ctx context.Context, apiKeyID int64, requestID string) (*AutoRoutingExecution, error)
	ClaimAutoRoutingAttemptDispatch(ctx context.Context, attempt *AutoRoutingAttempt) (bool, error)
	CompleteAutoRoutingAttempt(ctx context.Context, executionID int64, attemptNo int, resultReference string) (bool, error)
	MarkAutoRoutingAttemptUnknown(ctx context.Context, executionID int64, attemptNo int, evidenceCode string) (bool, error)
}

type autoRoutingAttemptTransitioner interface {
	PrepareNextAutoRoutingAttempt(ctx context.Context, executionID, attemptNo, nextGroupID int64) (bool, error)
	FailAutoRoutingAttempt(ctx context.Context, executionID int64, attemptNo int, evidenceCode string) (bool, error)
}

type AutoRoutingExecutionGuard struct {
	repo autoRoutingExecutionRepository
}

func NewAutoRoutingExecutionGuard(repo AutoRoutingRepository) *AutoRoutingExecutionGuard {
	if repo == nil {
		return nil
	}
	return &AutoRoutingExecutionGuard{repo: repo}
}

func HashAutoRoutingRequestBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (g *AutoRoutingExecutionGuard) Begin(ctx context.Context, request AutoRoutingExecutionRequest) (*AutoRoutingExecution, bool, error) {
	if g == nil || g.repo == nil {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable
	}
	if err := validateAutoRoutingExecutionIdentity(request.APIKeyID, request.RequestID); err != nil {
		return nil, false, err
	}
	if request.ProfileRevision <= 0 {
		return nil, false, ErrAutoRoutingExecutionInvalid
	}
	if execution, found, err := g.Lookup(ctx, request.APIKeyID, request.RequestID, request.Body); err != nil {
		return nil, false, err
	} else if found {
		return execution, false, nil
	}
	if err := validateAutoRoutingExecutionRequest(request); err != nil {
		return nil, false, err
	}

	input := AutoRoutingExecutionInput{
		APIKeyID:          request.APIKeyID,
		RequestID:         request.RequestID,
		PayloadHash:       HashAutoRoutingRequestBody(request.Body),
		ProfileID:         request.ProfileID,
		ProfileRevision:   request.ProfileRevision,
		RoutingRevision:   request.RoutingRevision,
		CandidateGroupIDs: append([]int64(nil), request.CandidateGroupIDs...),
		SelectedGroupID:   request.SelectedGroupID,
	}
	execution, created, err := g.repo.CreateOrGetAutoRoutingExecution(ctx, input)
	if err != nil {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if execution == nil || execution.ID <= 0 ||
		execution.APIKeyID != input.APIKeyID ||
		execution.RequestID != input.RequestID {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(errors.New("execution repository returned an invalid record"))
	}
	if execution.PayloadHash != input.PayloadHash {
		return nil, false, ErrAutoRoutingExecutionPayloadConflict
	}
	if err := validatePersistedAutoRoutingExecution(execution); err != nil {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if created && !sameAutoRoutingExecutionSnapshot(execution, input) {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(errors.New("execution repository persisted a different routing snapshot"))
	}

	if err := autoRoutingExecutionReplayError(execution); err != nil {
		return nil, false, err
	}
	return cloneAutoRoutingExecution(execution), created, nil
}

func (g *AutoRoutingExecutionGuard) Lookup(ctx context.Context, apiKeyID int64, requestID string, body []byte) (*AutoRoutingExecution, bool, error) {
	if g == nil || g.repo == nil {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable
	}
	if err := validateAutoRoutingExecutionIdentity(apiKeyID, requestID); err != nil {
		return nil, false, err
	}
	execution, err := g.repo.GetAutoRoutingExecution(ctx, apiKeyID, requestID)
	if err != nil {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if execution == nil {
		return nil, false, nil
	}
	if execution.ID <= 0 || execution.APIKeyID != apiKeyID || execution.RequestID != requestID {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(errors.New("execution repository returned a mismatched record"))
	}
	if execution.PayloadHash != HashAutoRoutingRequestBody(body) {
		return nil, false, ErrAutoRoutingExecutionPayloadConflict
	}
	if err := validatePersistedAutoRoutingExecution(execution); err != nil {
		return nil, false, ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if err := autoRoutingExecutionReplayError(execution); err != nil {
		return nil, false, err
	}
	return cloneAutoRoutingExecution(execution), true, nil
}

func (g *AutoRoutingExecutionGuard) ClaimDispatch(ctx context.Context, execution *AutoRoutingExecution, groupID int64) (*AutoRoutingExecution, error) {
	if g == nil || g.repo == nil {
		return nil, ErrAutoRoutingExecutionStoreUnavailable
	}
	if err := validatePreparedAutoRoutingExecution(execution); err != nil {
		return nil, err
	}
	if groupID <= 0 || execution.ActualGroupID == nil || *execution.ActualGroupID != groupID {
		return nil, ErrAutoRoutingExecutionInvalid
	}

	attempt := &AutoRoutingAttempt{
		ExecutionID: execution.ID,
		AttemptNo:   execution.CurrentAttempt + 1,
		GroupID:     groupID,
		Status:      AutoRouteAttemptPrepared,
	}
	claimed, err := g.repo.ClaimAutoRoutingAttemptDispatch(ctx, attempt)
	if err != nil {
		return nil, ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if !claimed {
		return nil, ErrAutoRoutingExecutionStateConflict
	}

	dispatched := cloneAutoRoutingExecution(execution)
	dispatched.Status = AutoRouteExecutionDispatching
	dispatched.CurrentAttempt = attempt.AttemptNo
	dispatched.ActualGroupID = int64Pointer(groupID)
	return dispatched, nil
}

func (g *AutoRoutingExecutionGuard) MarkCompleted(ctx context.Context, executionID int64, attemptNo int, resultReference string) error {
	if g == nil || g.repo == nil {
		return ErrAutoRoutingExecutionStoreUnavailable
	}
	if executionID <= 0 || attemptNo <= 0 || !validAutoRouteResultReference(resultReference) {
		return ErrAutoRoutingExecutionInvalid
	}
	completed, err := g.repo.CompleteAutoRoutingAttempt(ctx, executionID, attemptNo, strings.TrimSpace(resultReference))
	if err != nil {
		return ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if !completed {
		return ErrAutoRoutingExecutionStateConflict
	}
	return nil
}

func (g *AutoRoutingExecutionGuard) MarkUnknown(ctx context.Context, executionID int64, attemptNo int, evidenceCode string) error {
	if g == nil || g.repo == nil {
		return ErrAutoRoutingExecutionStoreUnavailable
	}
	if executionID <= 0 || attemptNo <= 0 || !validAutoRouteEvidenceCode(evidenceCode) {
		return ErrAutoRoutingExecutionInvalid
	}
	unknown, err := g.repo.MarkAutoRoutingAttemptUnknown(ctx, executionID, attemptNo, evidenceCode)
	if err != nil {
		return ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if !unknown {
		return ErrAutoRoutingExecutionStateConflict
	}
	return nil
}

func (g *AutoRoutingExecutionGuard) PrepareNextAttempt(ctx context.Context, executionID int64, attemptNo int, nextGroupID int64) error {
	if g == nil || g.repo == nil || executionID <= 0 || attemptNo <= 0 || nextGroupID <= 0 {
		return ErrAutoRoutingExecutionInvalid
	}
	transitioner, ok := g.repo.(autoRoutingAttemptTransitioner)
	if !ok {
		return ErrAutoRoutingExecutionStoreUnavailable
	}
	prepared, err := transitioner.PrepareNextAutoRoutingAttempt(ctx, executionID, int64(attemptNo), nextGroupID)
	if err != nil {
		return ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if !prepared {
		return ErrAutoRoutingExecutionStateConflict
	}
	return nil
}

func (g *AutoRoutingExecutionGuard) FailAttempt(ctx context.Context, executionID int64, attemptNo int, evidenceCode string) error {
	if g == nil || g.repo == nil || executionID <= 0 || attemptNo <= 0 || !validAutoRouteEvidenceCode(evidenceCode) {
		return ErrAutoRoutingExecutionInvalid
	}
	transitioner, ok := g.repo.(autoRoutingAttemptTransitioner)
	if !ok {
		return ErrAutoRoutingExecutionStoreUnavailable
	}
	failed, err := transitioner.FailAutoRoutingAttempt(ctx, executionID, attemptNo, evidenceCode)
	if err != nil {
		return ErrAutoRoutingExecutionStoreUnavailable.WithCause(err)
	}
	if !failed {
		return ErrAutoRoutingExecutionStateConflict
	}
	return nil
}

func BindAutoRoutingExecution(key *APIKey, execution *AutoRoutingExecution) (*APIKey, error) {
	if key == nil || execution == nil || key.ID <= 0 || execution.ID <= 0 {
		return nil, ErrAutoRoutingExecutionInvalid
	}
	if err := validatePersistedAutoRoutingExecution(execution); err != nil {
		return nil, ErrAutoRoutingExecutionInvalid.WithCause(err)
	}
	if key.ID != execution.APIKeyID ||
		key.GroupID == nil ||
		!containsAutoRouteGroup(execution.CandidateGroupIDs, *key.GroupID) {
		return nil, ErrAutoRoutingExecutionInvalid
	}
	if execution.Status == AutoRouteExecutionDispatching {
		if execution.CurrentAttempt <= 0 || execution.ActualGroupID == nil || *execution.ActualGroupID != *key.GroupID {
			return nil, ErrAutoRoutingExecutionInvalid
		}
	} else if execution.Status != AutoRouteExecutionPrepared ||
		execution.CurrentAttempt < 0 ||
		execution.ActualGroupID == nil ||
		*execution.ActualGroupID != *key.GroupID {
		return nil, ErrAutoRoutingExecutionInvalid
	}

	attemptNo := execution.CurrentAttempt
	if execution.Status == AutoRouteExecutionPrepared {
		attemptNo++
	}
	bound := *key
	bound.AutoRouting = &AutoRoutingMetadata{
		ProfileID:       execution.ProfileID,
		RoutingRevision: execution.RoutingRevision,
		ExecutionID:     execution.ID,
		AttemptNo:       attemptNo,
		RequestID:       execution.RequestID,
	}
	if key.AutoRouting != nil {
		bound.AutoRouting.PricingAt = key.AutoRouting.PricingAt
		if key.AutoRouting.RateMultiplier != nil {
			rate := *key.AutoRouting.RateMultiplier
			bound.AutoRouting.RateMultiplier = &rate
		}
	}
	return &bound, nil
}

func validateAutoRoutingExecutionRequest(request AutoRoutingExecutionRequest) error {
	if validateAutoRoutingExecutionIdentity(request.APIKeyID, request.RequestID) != nil ||
		request.ProfileID <= 0 ||
		request.ProfileRevision <= 0 ||
		request.RoutingRevision <= 0 ||
		len(request.CandidateGroupIDs) == 0 {
		return ErrAutoRoutingExecutionInvalid
	}
	seen := make(map[int64]struct{}, len(request.CandidateGroupIDs))
	for _, groupID := range request.CandidateGroupIDs {
		if groupID <= 0 {
			return ErrAutoRoutingExecutionInvalid
		}
		if _, exists := seen[groupID]; exists {
			return ErrAutoRoutingExecutionInvalid
		}
		seen[groupID] = struct{}{}
	}
	if request.SelectedGroupID <= 0 {
		return ErrAutoRoutingExecutionInvalid
	}
	if _, ok := seen[request.SelectedGroupID]; !ok {
		return ErrAutoRoutingExecutionInvalid
	}
	return nil
}

func validateAutoRoutingExecutionIdentity(apiKeyID int64, requestID string) error {
	if apiKeyID <= 0 || requestID == "" || strings.TrimSpace(requestID) != requestID {
		return ErrAutoRoutingExecutionInvalid
	}
	for _, character := range requestID {
		if unicode.IsControl(character) {
			return ErrAutoRoutingExecutionInvalid
		}
	}
	return nil
}

func validatePersistedAutoRoutingExecution(execution *AutoRoutingExecution) error {
	if execution.ID <= 0 || validateAutoRoutingExecutionIdentity(execution.APIKeyID, execution.RequestID) != nil ||
		execution.ProfileID <= 0 || execution.ProfileRevision <= 0 || execution.RoutingRevision <= 0 ||
		len(execution.CandidateGroupIDs) == 0 || execution.ActualGroupID == nil ||
		!containsAutoRouteGroup(execution.CandidateGroupIDs, *execution.ActualGroupID) {
		return errors.New("execution snapshot is incomplete")
	}
	if len(execution.PayloadHash) != sha256.Size*2 {
		return errors.New("execution payload hash is invalid")
	}
	decodedHash, err := hex.DecodeString(execution.PayloadHash)
	if err != nil || hex.EncodeToString(decodedHash) != execution.PayloadHash {
		return errors.New("execution payload hash is invalid")
	}
	seen := make(map[int64]struct{}, len(execution.CandidateGroupIDs))
	for _, groupID := range execution.CandidateGroupIDs {
		if groupID <= 0 {
			return errors.New("execution candidate snapshot contains an invalid group")
		}
		if _, exists := seen[groupID]; exists {
			return errors.New("execution candidate snapshot contains duplicate groups")
		}
		seen[groupID] = struct{}{}
	}
	return nil
}

func validatePreparedAutoRoutingExecution(execution *AutoRoutingExecution) error {
	if execution == nil || execution.ID <= 0 ||
		execution.Status != AutoRouteExecutionPrepared ||
		execution.CurrentAttempt < 0 ||
		execution.ActualGroupID == nil {
		return ErrAutoRoutingExecutionStateConflict
	}
	return validatePersistedAutoRoutingExecution(execution)
}

func autoRoutingExecutionReplayError(execution *AutoRoutingExecution) error {
	switch execution.Status {
	case AutoRouteExecutionPrepared:
		if execution.CurrentAttempt != 0 {
			return ErrAutoRoutingExecutionStoreUnavailable.WithCause(errors.New("prepared execution has an attempt"))
		}
		return nil
	case AutoRouteExecutionDispatching:
		return ErrAutoRoutingExecutionInProgress
	case AutoRouteExecutionUnknown:
		return ErrAutoRoutingExecutionUnknown
	case AutoRouteExecutionCompleted:
		return ErrAutoRoutingExecutionCompleted
	case AutoRouteExecutionFailed:
		return ErrAutoRoutingExecutionFailed
	default:
		return ErrAutoRoutingExecutionStoreUnavailable.WithCause(fmt.Errorf("unsupported persisted execution status %q", execution.Status))
	}
}

func sameAutoRoutingExecutionSnapshot(execution *AutoRoutingExecution, input AutoRoutingExecutionInput) bool {
	if execution.ProfileID != input.ProfileID ||
		execution.ProfileRevision != input.ProfileRevision ||
		execution.RoutingRevision != input.RoutingRevision ||
		execution.ActualGroupID == nil ||
		*execution.ActualGroupID != input.SelectedGroupID ||
		len(execution.CandidateGroupIDs) != len(input.CandidateGroupIDs) {
		return false
	}
	for index, groupID := range input.CandidateGroupIDs {
		if execution.CandidateGroupIDs[index] != groupID {
			return false
		}
	}
	return true
}

func containsAutoRouteGroup(groupIDs []int64, target int64) bool {
	for _, groupID := range groupIDs {
		if groupID == target {
			return true
		}
	}
	return false
}

func validAutoRouteResultReference(reference string) bool {
	reference = strings.TrimSpace(reference)
	if len(reference) > 512 {
		return false
	}
	for _, character := range reference {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validAutoRouteEvidenceCode(code string) bool {
	switch code {
	case AutoRouteEvidenceUpstreamTimeout,
		AutoRouteEvidenceConnectionInterrupted,
		AutoRouteEvidenceProcessRecovery,
		AutoRouteEvidenceClientCanceled,
		AutoRouteEvidenceResponseStarted,
		AutoRouteEvidenceUpstreamFailure:
		return true
	default:
		return false
	}
}

func cloneAutoRoutingExecution(execution *AutoRoutingExecution) *AutoRoutingExecution {
	if execution == nil {
		return nil
	}
	cloned := *execution
	cloned.CandidateGroupIDs = append([]int64(nil), execution.CandidateGroupIDs...)
	if execution.ActualGroupID != nil {
		cloned.ActualGroupID = int64Pointer(*execution.ActualGroupID)
	}
	if execution.ResultReference != nil {
		resultReference := *execution.ResultReference
		cloned.ResultReference = &resultReference
	}
	if execution.EvidenceCode != nil {
		evidenceCode := *execution.EvidenceCode
		cloned.EvidenceCode = &evidenceCode
	}
	if execution.CompletedAt != nil {
		completedAt := *execution.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	return &cloned
}

func int64Pointer(value int64) *int64 {
	return &value
}
