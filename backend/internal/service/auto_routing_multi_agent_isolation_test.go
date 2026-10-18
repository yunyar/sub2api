package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type autoRoutingMultiAgentExecutionKey struct {
	apiKeyID  int64
	requestID string
}

type autoRoutingMultiAgentExecutionRepo struct {
	AutoRoutingRepository
	base        *autoRoutingAcceptanceRoutingRepo
	mu          sync.Mutex
	executions  map[autoRoutingMultiAgentExecutionKey]*AutoRoutingExecution
	nextID      int64
	createCalls int
}

func (r *autoRoutingMultiAgentExecutionRepo) GetAutoRoutingProfile(ctx context.Context, id int64) (*AutoRoutingProfile, error) {
	return r.base.GetAutoRoutingProfile(ctx, id)
}

func (r *autoRoutingMultiAgentExecutionRepo) GetAPIKeyAutoRouting(ctx context.Context, id int64) (*APIKeyAutoRoutingConfig, error) {
	return r.base.GetAPIKeyAutoRouting(ctx, id)
}

func (r *autoRoutingMultiAgentExecutionRepo) GetAutoRoutingExecution(_ context.Context, apiKeyID int64, requestID string) (*AutoRoutingExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneAutoRoutingExecution(r.executions[autoRoutingMultiAgentExecutionKey{apiKeyID: apiKeyID, requestID: requestID}]), nil
}

func (r *autoRoutingMultiAgentExecutionRepo) CreateOrGetAutoRoutingExecution(
	_ context.Context,
	input AutoRoutingExecutionInput,
) (*AutoRoutingExecution, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	key := autoRoutingMultiAgentExecutionKey{apiKeyID: input.APIKeyID, requestID: input.RequestID}
	if execution := r.executions[key]; execution != nil {
		return cloneAutoRoutingExecution(execution), false, nil
	}
	r.nextID++
	execution := &AutoRoutingExecution{
		ID:                r.nextID,
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
	r.executions[key] = execution
	return cloneAutoRoutingExecution(execution), true, nil
}

func (r *autoRoutingMultiAgentExecutionRepo) createdExecutionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCalls
}

type autoRoutingMultiAgentRateRepo struct {
	UserGroupRateRepository
	mu         sync.Mutex
	rates      map[int64]float64
	rpms       map[int64]int
	errors     map[int64]error
	rateGroups []int64
	rpmGroups  []int64
}

func (r *autoRoutingMultiAgentRateRepo) GetByUserAndGroup(_ context.Context, _, groupID int64) (*float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rateGroups = append(r.rateGroups, groupID)
	if err := r.errors[groupID]; err != nil {
		return nil, err
	}
	rate, ok := r.rates[groupID]
	if !ok {
		return nil, nil
	}
	return &rate, nil
}

func (r *autoRoutingMultiAgentRateRepo) GetRPMOverrideByUserAndGroup(_ context.Context, _, groupID int64) (*int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rpmGroups = append(r.rpmGroups, groupID)
	rpm, ok := r.rpms[groupID]
	if !ok {
		return nil, nil
	}
	return &rpm, nil
}

func (r *autoRoutingMultiAgentRateRepo) queriedGroups() ([]int64, []int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.rateGroups...), append([]int64(nil), r.rpmGroups...)
}

type autoRoutingMultiAgentAccountRepo struct {
	AccountRepository
	accounts map[int64][]Account
}

func (r *autoRoutingMultiAgentAccountRepo) ListModelAvailabilityCandidates(
	_ context.Context,
	groupID *int64,
	_ []string,
	_ bool,
) ([]Account, error) {
	if groupID == nil {
		return nil, errors.New("expected group-scoped model lookup")
	}
	return append([]Account(nil), r.accounts[*groupID]...), nil
}

func newAutoRoutingMultiAgentIsolationFixture(
	failedRateGroupID int64,
) (autoRoutingAcceptanceFixture, *autoRoutingMultiAgentExecutionRepo, *autoRoutingMultiAgentRateRepo) {
	fixture := newAutoRoutingAcceptanceFixture()
	fixture.groups[202].Platform = PlatformAnthropic
	fixture.repo.config.APIKeyID = fixture.key.ID
	fixture.userRepo.user.AllowedGroups = []int64{101, 202}
	fixture.catalog.accountRepo = &autoRoutingMultiAgentAccountRepo{
		accounts: map[int64][]Account{
			101: {{
				ID: 1101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"model_mapping": map[string]any{"agent-openai": "openai-account-model"}},
			}},
			202: {{
				ID: 2202, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"model_mapping": map[string]any{"agent-anthropic": "anthropic-account-model"}},
			}},
		},
	}
	executionRepo := &autoRoutingMultiAgentExecutionRepo{
		AutoRoutingRepository: fixture.repo,
		base:                  fixture.repo,
		executions:            make(map[autoRoutingMultiAgentExecutionKey]*AutoRoutingExecution),
		nextID:                700,
	}
	fixture.routing.repo = executionRepo
	fixture.routing.executionGuard = NewAutoRoutingExecutionGuard(executionRepo)

	rateRepo := &autoRoutingMultiAgentRateRepo{
		rates:  map[int64]float64{101: 1.25, 202: 2.5},
		rpms:   map[int64]int{101: 11, 202: 22},
		errors: map[int64]error{},
	}
	if failedRateGroupID > 0 {
		rateRepo.errors[failedRateGroupID] = errors.New("test rate lookup failure")
	}
	fixture.routing.apiKeys.userGroupRateRepo = rateRepo
	return fixture, executionRepo, rateRepo
}

func TestAutoRoutingPrepareRequestKeyConcurrentModelsKeepGroupAndBillingRequestLocal(t *testing.T) {
	fixture, executionRepo, rateRepo := newAutoRoutingMultiAgentIsolationFixture(0)

	requests := []struct {
		modelID       string
		requestID     string
		groupID       int64
		platform      string
		upstreamModel string
		rate          float64
		rpm           int
		body          string
	}{
		{
			modelID: "agent-openai", requestID: "multi-agent:openai",
			groupID: 101, platform: PlatformOpenAI, upstreamModel: "openai-account-model",
			rate: 1.25, rpm: 11, body: `{"model":"agent-openai"}`,
		},
		{
			modelID: "agent-anthropic", requestID: "multi-agent:anthropic",
			groupID: 202, platform: PlatformAnthropic, upstreamModel: "anthropic-account-model",
			rate: 2.5, rpm: 22, body: `{"model":"agent-anthropic"}`,
		},
	}
	type result struct {
		request     int
		preparation *AutoRoutingPreparation
		err         error
	}
	start := make(chan struct{})
	results := make(chan result, len(requests))
	for index, request := range requests {
		index, request := index, request
		go func() {
			<-start
			preparation, err := fixture.routing.PrepareRequestKey(context.Background(), fixture.key, AutoRoutingRequest{
				Endpoint: AutoModelEndpointChat, ModelID: request.modelID, RequestID: request.requestID,
				Body: []byte(request.body),
			})
			results <- result{request: index, preparation: preparation, err: err}
		}()
	}
	close(start)

	preparations := make([]*AutoRoutingPreparation, len(requests))
	for range requests {
		result := <-results
		require.NoError(t, result.err)
		require.NotNil(t, result.preparation)
		preparations[result.request] = result.preparation
	}

	for index, request := range requests {
		preparation := preparations[index]
		require.Equal(t, request.modelID, preparation.Plan.ModelID)
		require.Equal(t, request.groupID, preparation.Plan.SelectedGroupID)
		require.Equal(t, request.groupID, *preparation.EffectiveKey.GroupID)
		require.Equal(t, request.platform, preparation.EffectiveKey.Group.Platform)
		require.Equal(t, request.rate, *preparation.EffectiveKey.AutoRouting.RateMultiplier)
		require.Equal(t, request.rpm, *preparation.EffectiveKey.User.UserGroupRPMOverride)
		require.NotSame(t, fixture.key, preparation.EffectiveKey)
		require.NotSame(t, fixture.key.User, preparation.EffectiveKey.User)
		require.NotSame(t, fixture.key.GroupID, preparation.EffectiveKey.GroupID)

		capability, supported, err := fixture.catalog.ResolveGroupModelCapability(
			context.Background(), preparation.EffectiveKey.Group, request.modelID, AutoModelEndpointChat,
		)
		require.NoError(t, err)
		require.True(t, supported)
		require.True(t, capability.AccountSchedulable)
		require.Equal(t, request.platform, capability.Platform)
		require.Equal(t, []string{request.upstreamModel}, capability.UpstreamModels)
	}

	require.NotEqual(t, preparations[0].Execution.ID, preparations[1].Execution.ID)
	require.Nil(t, fixture.key.GroupID, "auto preparation must not assign a routed group to the parent key")
	require.Nil(t, fixture.key.Group)
	require.Nil(t, fixture.key.AutoRouting)
	require.Equal(t, 2, executionRepo.createdExecutionCount())
	rateGroups, rpmGroups := rateRepo.queriedGroups()
	require.ElementsMatch(t, []int64{101, 202}, rateGroups)
	require.ElementsMatch(t, []int64{101, 202}, rpmGroups)
}

func TestAutoRoutingPrepareRequestKeyFailureDoesNotPoisonNextPlatform(t *testing.T) {
	fixture, executionRepo, _ := newAutoRoutingMultiAgentIsolationFixture(101)

	_, err := fixture.routing.PrepareRequestKey(context.Background(), fixture.key, AutoRoutingRequest{
		Endpoint: AutoModelEndpointChat, ModelID: "agent-openai",
		RequestID: "multi-agent:failed-openai", Body: []byte(`{"model":"agent-openai"}`),
	})
	require.ErrorIs(t, err, ErrAutoRoutingUnavailable)
	require.Nil(t, fixture.key.GroupID)
	require.Nil(t, fixture.key.Group)
	require.Nil(t, fixture.key.AutoRouting)

	preparation, err := fixture.routing.PrepareRequestKey(context.Background(), fixture.key, AutoRoutingRequest{
		Endpoint: AutoModelEndpointChat, ModelID: "agent-anthropic",
		RequestID: "multi-agent:next-anthropic", Body: []byte(`{"model":"agent-anthropic"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, preparation)
	require.Equal(t, int64(202), *preparation.EffectiveKey.GroupID)
	require.Equal(t, PlatformAnthropic, preparation.EffectiveKey.Group.Platform)
	require.Equal(t, "agent-anthropic", preparation.Plan.ModelID)
	require.Equal(t, 2, executionRepo.createdExecutionCount())
	require.Nil(t, fixture.key.GroupID, "failed and successful requests must leave the parent key untouched")
	require.Nil(t, fixture.key.Group)
	require.Nil(t, fixture.key.AutoRouting)
}
