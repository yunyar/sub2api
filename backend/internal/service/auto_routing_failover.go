package service

import (
	"context"
	"time"

	appconfig "github.com/Wei-Shaw/sub2api/internal/config"
)

// RetryNextGroup advances a confirmed, pre-response upstream failure to the
// next eligible candidate in the immutable Auto routing snapshot.
func (s *AutoRoutingService) RetryNextGroup(
	ctx context.Context,
	preparation *AutoRoutingPreparation,
	evidenceCode string,
) (*AutoRoutingPreparation, bool, error) {
	if s == nil || s.executionGuard == nil || preparation == nil ||
		preparation.Execution == nil || preparation.Plan == nil ||
		preparation.EffectiveKey == nil || preparation.EffectiveKey.AutoRouting == nil ||
		preparation.Execution.Status != AutoRouteExecutionDispatching ||
		preparation.Execution.ActualGroupID == nil {
		return nil, false, ErrAutoRoutingExecutionInvalid
	}
	config, err := s.getAPIKeyRouting(ctx, preparation.Execution.APIKeyID)
	if err != nil {
		return nil, false, err
	}
	if config == nil || !config.CrossGroupRetry {
		return nil, false, ErrAutoRoutingRequestUnsupported
	}
	profile, err := s.getProfile(ctx, config.ProfileID)
	if err != nil {
		return nil, false, err
	}
	if profile.Status != AutoRoutingProfileStatusActive || !profile.CrossGroupRetryAllowed {
		return nil, false, ErrAutoRoutingRequestUnsupported
	}
	candidates, user, err := s.resolveCandidates(ctx, preparation.Plan.UserID, profile, config)
	if err != nil {
		return nil, false, err
	}
	simpleMode := s.apiKeys.cfg != nil && s.apiKeys.cfg.RunMode == appconfig.RunModeSimple
	candidates = filterAutoRoutingExecutionCandidates(candidates, simpleMode, time.Now())
	candidateByGroup := make(map[int64]autoRoutingCandidate, len(candidates))
	for _, candidate := range candidates {
		if candidate.group != nil {
			candidateByGroup[candidate.group.ID] = candidate
		}
	}

	currentGroupID := *preparation.Execution.ActualGroupID
	currentPlatform := ""
	if preparation.EffectiveKey.Group != nil {
		currentPlatform = preparation.EffectiveKey.Group.Platform
	}
	currentIndex := -1
	for index, groupID := range preparation.Execution.CandidateGroupIDs {
		if groupID == currentGroupID {
			currentIndex = index
			break
		}
	}
	if currentIndex < 0 {
		return nil, false, ErrAutoRoutingExecutionInvalid
	}

	var selected autoRoutingCandidate
	for _, groupID := range preparation.Execution.CandidateGroupIDs[currentIndex+1:] {
		candidate, ok := candidateByGroup[groupID]
		if !ok || candidate.group == nil {
			continue
		}
		if currentPlatform != "" && candidate.group.Platform != currentPlatform {
			continue
		}
		capability, supported, resolveErr := s.catalog.ResolveGroupModelCapability(
			ctx, candidate.group, preparation.Plan.ModelID, preparation.Plan.Endpoint,
		)
		if resolveErr != nil {
			return nil, false, ErrAutoRoutingUnavailable.WithCause(resolveErr)
		}
		if supported && capability.AccountSchedulable {
			selected = candidate
			break
		}
	}
	if selected.group == nil {
		if err := s.executionGuard.FailAttempt(ctx, preparation.Execution.ID, preparation.Execution.CurrentAttempt, evidenceCode); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	execution := cloneAutoRoutingExecution(preparation.Execution)
	execution.Status = AutoRouteExecutionPrepared
	execution.ActualGroupID = int64Pointer(selected.group.ID)
	execution.ResultReference = nil
	execution.EvidenceCode = nil
	execution.CompletedAt = nil
	bound, err := bindPreparedAutoRoutingKey(preparation.EffectiveKey, user, selected, execution)
	if err != nil {
		return nil, false, err
	}
	if err := s.freezeAutoRoutingBilling(ctx, bound, selected.group, preparation.Plan.UserID); err != nil {
		return nil, false, err
	}
	if err := s.executionGuard.PrepareNextAttempt(
		ctx,
		preparation.Execution.ID,
		preparation.Execution.CurrentAttempt,
		selected.group.ID,
		evidenceCode,
	); err != nil {
		return nil, false, err
	}
	plan := *preparation.Plan
	plan.SelectedGroupID = selected.group.ID
	return &AutoRoutingPreparation{
		Plan:         &plan,
		Execution:    execution,
		EffectiveKey: bound,
		Subscription: cloneUserSubscription(selected.subscription),
	}, true, nil
}
