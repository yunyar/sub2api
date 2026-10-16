package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type autoRoutingExecutionRepoStub struct {
	AutoRoutingRepository
	mu          sync.Mutex
	execution   *AutoRoutingExecution
	getCalls    int
	createCalls int
}

func (r *autoRoutingExecutionRepoStub) CreateOrGetAutoRoutingExecution(_ context.Context, input AutoRoutingExecutionInput) (*AutoRoutingExecution, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	if r.execution != nil {
		return cloneAutoRoutingExecution(r.execution), false, nil
	}
	r.execution = &AutoRoutingExecution{
		ID:                41,
		APIKeyID:          input.APIKeyID,
		RequestID:         input.RequestID,
		PayloadHash:       input.PayloadHash,
		ProfileID:         input.ProfileID,
		ProfileRevision:   input.ProfileRevision,
		RoutingRevision:   input.RoutingRevision,
		CandidateGroupIDs: append([]int64(nil), input.CandidateGroupIDs...),
		Status:            AutoRouteExecutionPrepared,
		ActualGroupID:     int64Pointer(input.SelectedGroupID),
		SettlementStatus:  "pending",
	}
	return cloneAutoRoutingExecution(r.execution), true, nil
}

func (r *autoRoutingExecutionRepoStub) GetAutoRoutingExecution(_ context.Context, apiKeyID int64, requestID string) (*AutoRoutingExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getCalls++
	if r.execution == nil || r.execution.APIKeyID != apiKeyID || r.execution.RequestID != requestID {
		return nil, nil
	}
	return cloneAutoRoutingExecution(r.execution), nil
}

func (r *autoRoutingExecutionRepoStub) ClaimAutoRoutingAttemptDispatch(_ context.Context, attempt *AutoRoutingAttempt) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.execution == nil ||
		r.execution.ID != attempt.ExecutionID ||
		r.execution.Status != AutoRouteExecutionPrepared ||
		r.execution.CurrentAttempt != 0 ||
		r.execution.ActualGroupID == nil ||
		*r.execution.ActualGroupID != attempt.GroupID ||
		!containsAutoRouteGroup(r.execution.CandidateGroupIDs, attempt.GroupID) {
		return false, nil
	}
	r.execution.Status = AutoRouteExecutionDispatching
	r.execution.CurrentAttempt = attempt.AttemptNo
	return true, nil
}

func (r *autoRoutingExecutionRepoStub) CompleteAutoRoutingAttempt(_ context.Context, executionID int64, attemptNo int, resultReference string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.execution == nil ||
		r.execution.ID != executionID ||
		r.execution.Status != AutoRouteExecutionDispatching ||
		r.execution.CurrentAttempt != attemptNo {
		return false, nil
	}
	r.execution.Status = AutoRouteExecutionCompleted
	r.execution.ResultReference = &resultReference
	return true, nil
}

func (r *autoRoutingExecutionRepoStub) MarkAutoRoutingAttemptUnknown(_ context.Context, executionID int64, attemptNo int, evidenceCode string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.execution == nil ||
		r.execution.ID != executionID ||
		r.execution.Status != AutoRouteExecutionDispatching ||
		r.execution.CurrentAttempt != attemptNo {
		return false, nil
	}
	r.execution.Status = AutoRouteExecutionUnknown
	r.execution.EvidenceCode = &evidenceCode
	return true, nil
}

func TestHashAutoRoutingRequestBody(t *testing.T) {
	require.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", HashAutoRoutingRequestBody([]byte("hello")))
	require.NotEqual(t, HashAutoRoutingRequestBody([]byte("hello")), HashAutoRoutingRequestBody([]byte("hello ")))
}

func TestAutoRoutingExecutionGuardBeginReusesPreparedSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := &autoRoutingExecutionRepoStub{}
	guard := NewAutoRoutingExecutionGuard(repo)
	request := validAutoRoutingExecutionRequest()

	first, created, err := guard.Begin(ctx, request)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, int64(20), *first.ActualGroupID)
	require.Equal(t, "client:request-1", first.RequestID)

	changedRouting := request
	changedRouting.ProfileID = 9
	changedRouting.ProfileRevision = 8
	changedRouting.RoutingRevision = 7
	changedRouting.CandidateGroupIDs = []int64{30, 40}
	changedRouting.SelectedGroupID = 30
	replayed, created, err := guard.Begin(ctx, changedRouting)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ProfileID, replayed.ProfileID)
	require.Equal(t, first.ProfileRevision, replayed.ProfileRevision)
	require.Equal(t, first.RoutingRevision, replayed.RoutingRevision)
	require.Equal(t, first.CandidateGroupIDs, replayed.CandidateGroupIDs)
	require.Equal(t, *first.ActualGroupID, *replayed.ActualGroupID)
	require.Equal(t, 1, repo.createCalls)

	changedBody := changedRouting
	changedBody.Body = []byte(`{"prompt":"different"}`)
	_, _, err = guard.Begin(ctx, changedBody)
	require.ErrorIs(t, err, ErrAutoRoutingExecutionPayloadConflict)
	require.Equal(t, 1, repo.createCalls)
}

func TestAutoRoutingExecutionGuardRequiresProfileRevisionBeforeRepositoryLookup(t *testing.T) {
	repo := &autoRoutingExecutionRepoStub{}
	guard := NewAutoRoutingExecutionGuard(repo)
	request := validAutoRoutingExecutionRequest()
	request.ProfileRevision = 0

	_, _, err := guard.Begin(context.Background(), request)
	require.ErrorIs(t, err, ErrAutoRoutingExecutionInvalid)
	require.Zero(t, repo.getCalls)
	require.Zero(t, repo.createCalls)
}

func TestAutoRoutingExecutionGuardRejectsPersistedExecutionWithoutProfileRevision(t *testing.T) {
	request := validAutoRoutingExecutionRequest()
	repo := &autoRoutingExecutionRepoStub{}
	guard := NewAutoRoutingExecutionGuard(repo)
	execution := persistedExecutionFromRequest(request)
	execution.ProfileRevision = 0
	repo.execution = execution

	_, _, err := guard.Lookup(context.Background(), request.APIKeyID, request.RequestID, request.Body)
	require.ErrorIs(t, err, ErrAutoRoutingExecutionStoreUnavailable)
}

func TestAutoRoutingExecutionGuardReplayStatusesAreTerminalOrInProgress(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		attempt int
		wantErr error
	}{
		{name: "dispatching", status: AutoRouteExecutionDispatching, attempt: 1, wantErr: ErrAutoRoutingExecutionInProgress},
		{name: "unknown", status: AutoRouteExecutionUnknown, attempt: 1, wantErr: ErrAutoRoutingExecutionUnknown},
		{name: "completed", status: AutoRouteExecutionCompleted, attempt: 1, wantErr: ErrAutoRoutingExecutionCompleted},
		{name: "failed", status: AutoRouteExecutionFailed, attempt: 1, wantErr: ErrAutoRoutingExecutionFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validAutoRoutingExecutionRequest()
			repo := &autoRoutingExecutionRepoStub{
				execution: persistedExecutionFromRequest(request),
			}
			repo.execution.Status = test.status
			repo.execution.CurrentAttempt = test.attempt
			guard := NewAutoRoutingExecutionGuard(repo)

			_, _, err := guard.Begin(context.Background(), request)
			require.ErrorIs(t, err, test.wantErr)
			require.Zero(t, repo.createCalls)
		})
	}
}

func TestAutoRoutingExecutionGuardClaimDispatchIsOneShot(t *testing.T) {
	request := validAutoRoutingExecutionRequest()
	repo := &autoRoutingExecutionRepoStub{}
	guard := NewAutoRoutingExecutionGuard(repo)
	prepared, created, err := guard.Begin(context.Background(), request)
	require.NoError(t, err)
	require.True(t, created)

	const callers = 16
	var wait sync.WaitGroup
	var successes int
	var conflicts int
	var resultMu sync.Mutex
	var dispatchedResults []*AutoRoutingExecution
	wait.Add(callers)
	for caller := 0; caller < callers; caller++ {
		go func() {
			defer wait.Done()
			dispatched, claimErr := guard.ClaimDispatch(context.Background(), prepared, *prepared.ActualGroupID)
			resultMu.Lock()
			defer resultMu.Unlock()
			if dispatched != nil {
				dispatchedResults = append(dispatchedResults, dispatched)
			}
			if claimErr == nil {
				successes++
				return
			}
			if errors.Is(claimErr, ErrAutoRoutingExecutionStateConflict) {
				conflicts++
			}
		}()
	}
	wait.Wait()

	require.Equal(t, 1, successes)
	require.Equal(t, callers-1, conflicts)
	require.Len(t, dispatchedResults, 1)
	require.Equal(t, AutoRouteExecutionDispatching, dispatchedResults[0].Status)
	require.Equal(t, 1, dispatchedResults[0].CurrentAttempt)
}

func TestAutoRoutingExecutionGuardTerminalTransitions(t *testing.T) {
	tests := []struct {
		name       string
		transition func(*AutoRoutingExecutionGuard, int64) error
		wantState  string
	}{
		{
			name: "completed",
			transition: func(guard *AutoRoutingExecutionGuard, executionID int64) error {
				return guard.MarkCompleted(context.Background(), executionID, 1, " result-1 ")
			},
			wantState: AutoRouteExecutionCompleted,
		},
		{
			name: "unknown",
			transition: func(guard *AutoRoutingExecutionGuard, executionID int64) error {
				return guard.MarkUnknown(context.Background(), executionID, 1, AutoRouteEvidenceUpstreamTimeout)
			},
			wantState: AutoRouteExecutionUnknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &autoRoutingExecutionRepoStub{}
			guard := NewAutoRoutingExecutionGuard(repo)
			prepared, _, err := guard.Begin(context.Background(), validAutoRoutingExecutionRequest())
			require.NoError(t, err)
			_, err = guard.ClaimDispatch(context.Background(), prepared, *prepared.ActualGroupID)
			require.NoError(t, err)

			require.NoError(t, test.transition(guard, prepared.ID))
			require.Equal(t, test.wantState, repo.execution.Status)
			require.Equal(t, "pending", repo.execution.SettlementStatus)
			if test.wantState == AutoRouteExecutionCompleted {
				require.Equal(t, "result-1", *repo.execution.ResultReference)
			} else {
				require.Equal(t, AutoRouteEvidenceUpstreamTimeout, *repo.execution.EvidenceCode)
			}
		})
	}
}

func TestBindAutoRoutingExecutionAcceptsPreparedSelectedCandidate(t *testing.T) {
	execution := persistedExecutionFromRequest(validAutoRoutingExecutionRequest())
	key := &APIKey{ID: execution.APIKeyID, GroupID: int64Pointer(*execution.ActualGroupID)}

	bound, err := BindAutoRoutingExecution(key, execution)

	require.NoError(t, err)
	require.NotSame(t, key, bound)
	require.NotNil(t, bound.AutoRouting)
	require.Equal(t, execution.ProfileID, bound.AutoRouting.ProfileID)
	require.Equal(t, execution.RoutingRevision, bound.AutoRouting.RoutingRevision)
	require.Equal(t, execution.ID, bound.AutoRouting.ExecutionID)
	require.Equal(t, 1, bound.AutoRouting.AttemptNo)
	require.Equal(t, execution.RequestID, bound.AutoRouting.RequestID)
	require.Nil(t, key.AutoRouting)
}

func TestAutoRoutingExecutionGuardRejectsInvalidSelectionAndEvidence(t *testing.T) {
	request := validAutoRoutingExecutionRequest()
	repo := &autoRoutingExecutionRepoStub{}
	guard := NewAutoRoutingExecutionGuard(repo)
	prepared, _, err := guard.Begin(context.Background(), request)
	require.NoError(t, err)

	_, err = guard.ClaimDispatch(context.Background(), prepared, 99)
	require.ErrorIs(t, err, ErrAutoRoutingExecutionInvalid)

	key := &APIKey{ID: request.APIKeyID, GroupID: int64Pointer(99)}
	_, err = BindAutoRoutingExecution(key, prepared)
	require.ErrorIs(t, err, ErrAutoRoutingExecutionInvalid)

	outOfSnapshot := cloneAutoRoutingExecution(prepared)
	outOfSnapshot.ActualGroupID = int64Pointer(99)
	outOfSnapshotKey := &APIKey{ID: request.APIKeyID, GroupID: int64Pointer(99)}
	_, err = BindAutoRoutingExecution(outOfSnapshotKey, outOfSnapshot)
	require.ErrorIs(t, err, ErrAutoRoutingExecutionInvalid)

	require.ErrorIs(t, guard.MarkUnknown(context.Background(), prepared.ID, 1, "http_500"), ErrAutoRoutingExecutionInvalid)
	require.ErrorIs(t, guard.MarkUnknown(context.Background(), prepared.ID, 1, AutoRouteEvidenceUpstreamTimeout), ErrAutoRoutingExecutionStateConflict)
	require.ErrorIs(t, guard.MarkCompleted(context.Background(), prepared.ID, 1, "bad\nreference"), ErrAutoRoutingExecutionInvalid)
}

func validAutoRoutingExecutionRequest() AutoRoutingExecutionRequest {
	return AutoRoutingExecutionRequest{
		APIKeyID:          7,
		RequestID:         "client:request-1",
		Body:              []byte(`{"prompt":"hello"}`),
		ProfileID:         3,
		ProfileRevision:   2,
		RoutingRevision:   5,
		CandidateGroupIDs: []int64{20, 30},
		SelectedGroupID:   20,
	}
}

func persistedExecutionFromRequest(request AutoRoutingExecutionRequest) *AutoRoutingExecution {
	return &AutoRoutingExecution{
		ID:                41,
		APIKeyID:          request.APIKeyID,
		RequestID:         request.RequestID,
		PayloadHash:       HashAutoRoutingRequestBody(request.Body),
		ProfileID:         request.ProfileID,
		ProfileRevision:   request.ProfileRevision,
		RoutingRevision:   request.RoutingRevision,
		CandidateGroupIDs: append([]int64(nil), request.CandidateGroupIDs...),
		Status:            AutoRouteExecutionPrepared,
		ActualGroupID:     int64Pointer(request.SelectedGroupID),
		SettlementStatus:  "pending",
	}
}
