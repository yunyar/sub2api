package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	appconfig "github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAutoRoutingUnavailable = infraerrors.ServiceUnavailable(
		"AUTO_ROUTING_UNAVAILABLE",
		"auto routing is temporarily unavailable",
	)
	ErrAutoRoutingProfileInvalid = infraerrors.BadRequest(
		"AUTO_ROUTING_PROFILE_INVALID",
		"auto routing profile is invalid",
	)
	ErrAutoRoutingProfileNotFound = infraerrors.NotFound(
		"AUTO_ROUTING_PROFILE_NOT_FOUND",
		"auto routing profile not found",
	)
	ErrAutoRoutingKeyNotFound = infraerrors.NotFound(
		"API_KEY_NOT_FOUND",
		"api key not found",
	)
	ErrAutoRoutingCandidateUnavailable = infraerrors.NotFound(
		"AUTO_ROUTING_CANDIDATE_UNAVAILABLE",
		"no authorized group can serve this model and endpoint",
	)
	ErrAutoRoutingCapacityUnavailable = infraerrors.ServiceUnavailable(
		"AUTO_ROUTING_CAPACITY_UNAVAILABLE",
		"no authorized group currently has a schedulable account for this model and endpoint",
	)
	ErrAutoRoutingRequestUnsupported = infraerrors.BadRequest(
		"AUTO_ROUTING_REQUEST_UNSUPPORTED",
		"this request is not supported with auto routing",
	)
)

const (
	AutoRoutingModeFixed            = "fixed"
	maxAutoRoutingProfileCandidates = 50
)

type AutoRoutingService struct {
	apiKeys         *APIKeyService
	groupRepo       GroupRepository
	repo            AutoRoutingRepository
	catalog         *AutoModelCatalog
	executionGuard  *AutoRoutingExecutionGuard
	dispatchBilling *BillingCacheService
}

type AutoRoutingRequest struct {
	IsModelDirectory bool
	Endpoint         AutoModelEndpoint
	ModelID          string
	RequestID        string
	Body             []byte
}

type AutoRoutingPlan struct {
	APIKeyID          int64
	UserID            int64
	ProfileID         int64
	ProfileRevision   int64
	RoutingRevision   int64
	CandidateGroupIDs []int64
	SelectedGroupID   int64
	ModelID           string
	Endpoint          AutoModelEndpoint
}

type AutoRoutingPreparation struct {
	Plan         *AutoRoutingPlan
	Execution    *AutoRoutingExecution
	EffectiveKey *APIKey
	Subscription *UserSubscription
	Capabilities []AutoModelCapability
}

type APIKeyRoutingState struct {
	Mode         string
	Revision     int64
	FixedGroupID *int64
	Auto         *APIKeyAutoRoutingConfig
}

func NewAutoRoutingService(
	apiKeys *APIKeyService,
	groups GroupRepository,
	repo AutoRoutingRepository,
	catalog *AutoModelCatalog,
) *AutoRoutingService {
	return &AutoRoutingService{
		apiKeys:        apiKeys,
		groupRepo:      groups,
		repo:           repo,
		catalog:        catalog,
		executionGuard: NewAutoRoutingExecutionGuard(repo),
	}
}

func (s *AutoRoutingService) ListProfiles(ctx context.Context) ([]AutoRoutingProfile, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	profiles, err := s.repo.ListAutoRoutingProfiles(ctx)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	for i := range profiles {
		profiles[i] = cloneAutoRoutingProfile(&profiles[i])
	}
	return profiles, nil
}

func (s *AutoRoutingService) GetProfile(ctx context.Context, id int64) (*AutoRoutingProfile, error) {
	profile, err := s.getProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *AutoRoutingService) CreateProfile(ctx context.Context, input *AutoRoutingProfile) (*AutoRoutingProfile, error) {
	if s == nil || s.repo == nil || input == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	profile := cloneAutoRoutingProfile(input)
	profile.Status = AutoRoutingProfileStatusDisabled
	if err := s.validateProfile(ctx, &profile); err != nil {
		return nil, err
	}
	profile.ID = 0
	profile.Revision = 0
	if err := s.repo.CreateAutoRoutingProfile(ctx, &profile); err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	return &profile, nil
}

func (s *AutoRoutingService) UpdateProfile(
	ctx context.Context,
	input *AutoRoutingProfile,
	expectedRevision int64,
) (*AutoRoutingProfile, error) {
	if s == nil || s.repo == nil || input == nil || input.ID <= 0 || expectedRevision <= 0 {
		return nil, ErrAutoRoutingProfileInvalid
	}
	current, err := s.getProfile(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	profile := cloneAutoRoutingProfile(input)
	if profile.ModelCatalogFilter == nil {
		profile.ModelCatalogFilter = cloneAutoRoutingModelCatalogFilter(current.ModelCatalogFilter)
	}
	profile.CreatedAt = current.CreatedAt
	profile.UpdatedAt = current.UpdatedAt
	if profile.Status == "" {
		profile.Status = current.Status
	}
	if err := s.validateProfile(ctx, &profile); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateAutoRoutingProfile(ctx, &profile, expectedRevision); err != nil {
		if errors.Is(err, ErrAutoRoutingRevisionConflict) {
			return nil, ErrAutoRoutingRevisionConflict
		}
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	return &profile, nil
}

func (s *AutoRoutingService) DisableProfile(ctx context.Context, id, expectedRevision int64) (*AutoRoutingProfile, error) {
	if s == nil || s.repo == nil || id <= 0 || expectedRevision <= 0 {
		return nil, ErrAutoRoutingProfileInvalid
	}
	profile, err := s.getProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	profile.Status = AutoRoutingProfileStatusDisabled
	if err := s.repo.UpdateAutoRoutingProfile(ctx, profile, expectedRevision); err != nil {
		if errors.Is(err, ErrAutoRoutingRevisionConflict) {
			return nil, ErrAutoRoutingRevisionConflict
		}
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	return profile, nil
}

func (s *AutoRoutingService) AvailableProfiles(ctx context.Context, userID int64) ([]AutoRoutingProfile, error) {
	if s == nil || s.apiKeys == nil || s.repo == nil || userID <= 0 {
		return nil, ErrAutoRoutingUnavailable
	}
	groups, err := s.apiKeys.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	bindable := make(map[int64]struct{}, len(groups))
	for i := range groups {
		bindable[groups[i].ID] = struct{}{}
	}
	profiles, err := s.repo.ListAutoRoutingProfiles(ctx)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	available := make([]AutoRoutingProfile, 0, len(profiles))
	for i := range profiles {
		profile := profiles[i]
		if profile.Status != AutoRoutingProfileStatusActive {
			continue
		}
		members := make([]AutoRoutingProfileMember, 0, len(profile.Members))
		for _, member := range profile.Members {
			if _, ok := bindable[member.GroupID]; ok {
				members = append(members, member)
			}
		}
		if len(members) == 0 {
			continue
		}
		profile.Members = members
		available = append(available, cloneAutoRoutingProfile(&profile))
	}
	return available, nil
}

func (s *AutoRoutingService) GetKeyRouting(ctx context.Context, userID, keyID int64) (*APIKeyAutoRoutingConfig, error) {
	key, err := s.getOwnedKey(ctx, userID, keyID)
	if err != nil {
		return nil, err
	}
	config, err := s.getAPIKeyRouting(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if err := validateAPIKeyRoutingState(key, config); err != nil {
		return nil, err
	}
	return config, nil
}

func (s *AutoRoutingService) GetAPIKeyRouting(ctx context.Context, userID, keyID int64) (*APIKeyAutoRoutingConfig, error) {
	return s.GetKeyRouting(ctx, userID, keyID)
}

func (s *AutoRoutingService) AutoKeyConfig(ctx context.Context, keyID int64) (*APIKeyAutoRoutingConfig, error) {
	if s == nil || s.apiKeys == nil || keyID <= 0 {
		return nil, ErrAutoRoutingKeyNotFound
	}
	key, err := s.apiKeys.GetByID(ctx, keyID)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if key == nil || key.RoutingMode != AutoRoutingModeAuto {
		return nil, ErrAutoRoutingRequestUnsupported
	}
	config, err := s.getAPIKeyRouting(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if err := validateAPIKeyRoutingState(key, config); err != nil {
		return nil, err
	}
	return config, nil
}

func (s *AutoRoutingService) GetKeyRoutingState(ctx context.Context, userID, keyID int64) (*APIKeyRoutingState, error) {
	key, err := s.getOwnedKey(ctx, userID, keyID)
	if err != nil {
		return nil, err
	}
	config, err := s.getAPIKeyRouting(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if err := validateAPIKeyRoutingState(key, config); err != nil {
		return nil, err
	}
	state := &APIKeyRoutingState{Mode: AutoRoutingModeFixed}
	state.Revision = key.RoutingRevision
	if key.GroupID != nil {
		state.FixedGroupID = int64Pointer(*key.GroupID)
	}
	if config != nil {
		state.Mode = AutoRoutingModeAuto
		state.Auto = config
		state.FixedGroupID = nil
	}
	return state, nil
}

func (s *AutoRoutingService) UpdateKeyRouting(
	ctx context.Context,
	userID, keyID int64,
	config *APIKeyAutoRoutingConfig,
	fixedGroupID *int64,
	expectedRevision int64,
) (*APIKeyAutoRoutingConfig, error) {
	key, err := s.getOwnedKey(ctx, userID, keyID)
	if err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	if expectedRevision < 0 {
		return nil, ErrAutoRoutingProfileInvalid
	}
	currentConfig, err := s.getAPIKeyRouting(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if err := validateAPIKeyRoutingState(key, currentConfig); err != nil {
		return nil, err
	}
	if key.RoutingRevision != expectedRevision {
		return nil, ErrAutoRoutingRevisionConflict
	}
	if config == nil {
		if err := s.validateFixedGroup(ctx, userID, fixedGroupID); err != nil {
			return nil, err
		}
	} else {
		if fixedGroupID != nil {
			return nil, ErrAutoRoutingProfileInvalid
		}
		if err := s.validateKeyRouting(ctx, userID, config); err != nil {
			return nil, err
		}
		config = cloneAPIKeyAutoRoutingConfig(config)
		config.APIKeyID = keyID
	}
	if err := s.repo.ReplaceAPIKeyAutoRouting(ctx, keyID, config, fixedGroupID, expectedRevision); err != nil {
		if errors.Is(err, ErrAutoRoutingRevisionConflict) {
			return nil, ErrAutoRoutingRevisionConflict
		}
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	s.apiKeys.InvalidateAuthCacheByKey(ctx, key.Key)
	if config == nil {
		return nil, nil
	}
	updated, err := s.getAPIKeyRouting(ctx, keyID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *AutoRoutingService) PrepareRequestKey(
	ctx context.Context,
	authenticatedKey *APIKey,
	request AutoRoutingRequest,
) (*AutoRoutingPreparation, error) {
	if s == nil || s.repo == nil || s.groupRepo == nil || s.apiKeys == nil || authenticatedKey == nil ||
		authenticatedKey.ID <= 0 || authenticatedKey.UserID <= 0 {
		return nil, ErrAutoRoutingUnavailable
	}
	config, err := s.getAPIKeyRouting(ctx, authenticatedKey.ID)
	if err != nil {
		return nil, err
	}
	if authenticatedKey.RoutingMode != AutoRoutingModeAuto {
		return nil, ErrAutoRoutingRequestUnsupported
	}
	if err := validateAPIKeyRoutingState(authenticatedKey, config); err != nil {
		return nil, err
	}
	if config == nil {
		return nil, ErrAutoRoutingRequestUnsupported
	}
	profile, err := s.getProfile(ctx, config.ProfileID)
	if err != nil {
		return nil, err
	}
	if profile.Status != AutoRoutingProfileStatusActive {
		return nil, ErrAutoRoutingRequestUnsupported
	}
	candidates, user, err := s.resolveCandidates(ctx, authenticatedKey.UserID, profile, config)
	if err != nil {
		return nil, err
	}
	if request.IsModelDirectory {
		capabilities, err := s.resolveDirectory(ctx, candidates, request.ModelID)
		if err != nil {
			return nil, err
		}
		capabilities = filterAutoModelCapabilities(capabilities, profile.ModelCatalogFilter)
		return &AutoRoutingPreparation{
			Plan: &AutoRoutingPlan{
				APIKeyID: authenticatedKey.ID, UserID: authenticatedKey.UserID,
				ProfileID: profile.ID, ProfileRevision: profile.Revision,
				RoutingRevision: config.Revision, CandidateGroupIDs: candidateIDs(candidates),
				ModelID: request.ModelID,
			},
			Capabilities: capabilities,
		}, nil
	}
	if err := validateAutoRoutingRequest(request); err != nil {
		return nil, err
	}
	if request.Endpoint == AutoModelEndpointResponses && autoRequestUsesImageGeneration(request.Body) {
		return nil, ErrAutoRoutingRequestUnsupported
	}
	simpleMode := s.apiKeys.cfg != nil && s.apiKeys.cfg.RunMode == appconfig.RunModeSimple
	candidates = filterAutoRoutingExecutionCandidates(candidates, simpleMode, time.Now())
	if len(candidates) == 0 {
		return nil, ErrAutoRoutingCandidateUnavailable
	}
	if s.catalog == nil {
		return nil, ErrAutoModelCatalogUnavailable
	}

	var execution *AutoRoutingExecution
	if existing, found, lookupErr := s.executionGuard.Lookup(ctx, authenticatedKey.ID, request.RequestID, request.Body); lookupErr != nil {
		return nil, lookupErr
	} else if found {
		execution = existing
	}

	var selected autoRoutingCandidate
	if execution != nil {
		selected, err = candidateByID(candidates, execution.ActualGroupID)
		if err != nil {
			return nil, err
		}
		capability, ok, resolveErr := s.catalog.ResolveGroupModelCapability(ctx, selected.group, request.ModelID, request.Endpoint)
		if resolveErr != nil {
			return nil, ErrAutoRoutingUnavailable.WithCause(resolveErr)
		} else if !ok || !capability.AccountSchedulable {
			return nil, ErrAutoRoutingCandidateUnavailable
		}
	} else {
		staticallySupported := false
		for _, candidate := range candidates {
			capability, supported, resolveErr := s.catalog.ResolveGroupModelCapability(ctx, candidate.group, request.ModelID, request.Endpoint)
			if resolveErr != nil {
				return nil, ErrAutoRoutingUnavailable.WithCause(resolveErr)
			}
			if !supported {
				continue
			}
			staticallySupported = true
			if capability.AccountSchedulable {
				selected = candidate
				break
			}
		}
		if selected.group == nil {
			if staticallySupported {
				return nil, ErrAutoRoutingCapacityUnavailable
			}
			return nil, ErrAutoRoutingCandidateUnavailable
		}
		execution, _, err = s.executionGuard.Begin(ctx, AutoRoutingExecutionRequest{
			APIKeyID: authenticatedKey.ID, RequestID: request.RequestID, Body: request.Body,
			ProfileID: profile.ID, ProfileRevision: profile.Revision,
			RoutingRevision: config.Revision, CandidateGroupIDs: candidateIDs(candidates),
			SelectedGroupID: selected.group.ID,
		})
		if err != nil {
			return nil, err
		}
		selected, err = candidateByID(candidates, execution.ActualGroupID)
		if err != nil {
			return nil, err
		}
		capability, supported, resolveErr := s.catalog.ResolveGroupModelCapability(ctx, selected.group, request.ModelID, request.Endpoint)
		if resolveErr != nil {
			return nil, ErrAutoRoutingUnavailable.WithCause(resolveErr)
		}
		if !supported || !capability.AccountSchedulable {
			return nil, ErrAutoRoutingCandidateUnavailable
		}
	}

	plan := &AutoRoutingPlan{
		APIKeyID: authenticatedKey.ID, UserID: authenticatedKey.UserID,
		ProfileID: execution.ProfileID, ProfileRevision: execution.ProfileRevision,
		RoutingRevision:   execution.RoutingRevision,
		CandidateGroupIDs: append([]int64(nil), execution.CandidateGroupIDs...),
		SelectedGroupID:   selected.group.ID, ModelID: request.ModelID, Endpoint: request.Endpoint,
	}
	bound, err := bindPreparedAutoRoutingKey(authenticatedKey, user, selected, execution)
	if err != nil {
		return nil, err
	}
	if err := s.freezeAutoRoutingBilling(ctx, bound, selected.group, authenticatedKey.UserID); err != nil {
		return nil, err
	}
	return &AutoRoutingPreparation{
		Plan: plan, Execution: cloneAutoRoutingExecution(execution),
		EffectiveKey: bound, Subscription: cloneUserSubscription(selected.subscription),
	}, nil
}

func (s *AutoRoutingService) ClaimDispatch(
	ctx context.Context,
	preparation *AutoRoutingPreparation,
) (*AutoRoutingPreparation, error) {
	if s == nil || s.executionGuard == nil || preparation == nil ||
		preparation.Execution == nil || preparation.EffectiveKey == nil ||
		preparation.EffectiveKey.GroupID == nil {
		return nil, ErrAutoRoutingExecutionInvalid
	}
	dispatched, err := s.executionGuard.ClaimDispatch(
		ctx, preparation.Execution, *preparation.EffectiveKey.GroupID,
	)
	if err != nil {
		return nil, err
	}
	boundKey, err := BindAutoRoutingExecution(preparation.EffectiveKey, dispatched)
	if err != nil {
		return nil, err
	}
	boundKey = cloneAutoRoutingAPIKey(boundKey)
	prepared := *preparation
	prepared.Execution = dispatched
	prepared.EffectiveKey = boundKey
	if preparation.Plan != nil {
		plan := *preparation.Plan
		plan.CandidateGroupIDs = append([]int64(nil), preparation.Plan.CandidateGroupIDs...)
		prepared.Plan = &plan
	}
	return &prepared, nil
}

func (s *AutoRoutingService) MarkCompleted(
	ctx context.Context,
	preparation *AutoRoutingPreparation,
	resultReference string,
) error {
	if s == nil || s.executionGuard == nil || preparation == nil || preparation.Execution == nil ||
		preparation.Execution.Status != AutoRouteExecutionDispatching ||
		strings.TrimSpace(resultReference) == "" {
		return ErrAutoRoutingExecutionInvalid
	}
	if err := s.executionGuard.MarkCompleted(ctx, preparation.Execution.ID, preparation.Execution.CurrentAttempt, resultReference); err != nil {
		return err
	}
	preparation.Execution.Status = AutoRouteExecutionCompleted
	return nil
}

func (s *AutoRoutingService) Complete(
	ctx context.Context,
	preparation *AutoRoutingPreparation,
	verifiedResultReference string,
) error {
	return s.MarkCompleted(ctx, preparation, verifiedResultReference)
}

func (s *AutoRoutingService) MarkUnknown(
	ctx context.Context,
	preparation *AutoRoutingPreparation,
	evidenceCode string,
) error {
	if s == nil || s.executionGuard == nil || preparation == nil || preparation.Execution == nil ||
		preparation.Execution.Status != AutoRouteExecutionDispatching {
		return ErrAutoRoutingExecutionInvalid
	}
	if err := s.executionGuard.MarkUnknown(ctx, preparation.Execution.ID, preparation.Execution.CurrentAttempt, evidenceCode); err != nil {
		return err
	}
	preparation.Execution.Status = AutoRouteExecutionUnknown
	return nil
}

func (s *AutoRoutingService) getAPIKeyRouting(ctx context.Context, keyID int64) (*APIKeyAutoRoutingConfig, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	config, err := s.repo.GetAPIKeyAutoRouting(ctx, keyID)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if config == nil {
		return nil, nil
	}
	if config.APIKeyID != keyID || config.Mode != AutoRoutingModeAuto || config.ProfileID <= 0 ||
		config.Revision <= 0 ||
		(config.SelectionMode != AutoRoutingSelectionInherit && config.SelectionMode != AutoRoutingSelectionCustom) ||
		(config.SelectionMode == AutoRoutingSelectionInherit && len(config.GroupIDs) != 0) ||
		(config.SelectionMode == AutoRoutingSelectionCustom && len(config.GroupIDs) == 0) {
		return nil, ErrAutoRoutingUnavailable
	}
	return cloneAPIKeyAutoRoutingConfig(config), nil
}

func validateAPIKeyRoutingState(key *APIKey, config *APIKeyAutoRoutingConfig) error {
	if key == nil || key.RoutingRevision < 0 {
		return ErrAutoRoutingUnavailable
	}
	switch key.RoutingMode {
	case AutoRoutingModeFixed:
		if config != nil {
			return ErrAutoRoutingUnavailable
		}
	case AutoRoutingModeAuto:
		if config == nil || key.GroupID != nil || key.RoutingRevision <= 0 ||
			config.Revision != key.RoutingRevision {
			return ErrAutoRoutingUnavailable
		}
	default:
		return ErrAutoRoutingUnavailable
	}
	return nil
}

func (s *AutoRoutingService) freezeAutoRoutingBilling(
	ctx context.Context,
	key *APIKey,
	group *Group,
	userID int64,
) error {
	if s == nil || s.apiKeys == nil || s.apiKeys.userGroupRateRepo == nil ||
		key == nil || key.AutoRouting == nil || key.User == nil || group == nil || group.ID <= 0 || userID <= 0 {
		return ErrAutoRoutingUnavailable
	}
	rateMultiplier := group.RateMultiplier
	userRate, err := s.apiKeys.userGroupRateRepo.GetByUserAndGroup(ctx, userID, group.ID)
	if err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	if userRate != nil {
		rateMultiplier = *userRate
	}
	rpmOverride, err := s.apiKeys.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, userID, group.ID)
	if err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	metadata := *key.AutoRouting
	metadata.RateMultiplier = &rateMultiplier
	metadata.PricingAt = time.Now()
	key.AutoRouting = &metadata
	key.User.UserGroupRPMOverride = nil
	if rpmOverride != nil {
		override := *rpmOverride
		key.User.UserGroupRPMOverride = &override
	}
	return nil
}

func (s *AutoRoutingService) getProfile(ctx context.Context, id int64) (*AutoRoutingProfile, error) {
	if s == nil || s.repo == nil || id <= 0 {
		return nil, ErrAutoRoutingProfileInvalid
	}
	profile, err := s.repo.GetAutoRoutingProfile(ctx, id)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if profile == nil {
		return nil, ErrAutoRoutingProfileNotFound
	}
	cloned := cloneAutoRoutingProfile(profile)
	return &cloned, nil
}

func (s *AutoRoutingService) getOwnedKey(ctx context.Context, userID, keyID int64) (*APIKey, error) {
	if s == nil || s.apiKeys == nil || userID <= 0 || keyID <= 0 {
		return nil, ErrAutoRoutingKeyNotFound
	}
	key, err := s.apiKeys.GetByID(ctx, keyID)
	if err != nil {
		return nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if key == nil || key.UserID != userID {
		return nil, ErrAutoRoutingKeyNotFound
	}
	return key, nil
}

func (s *AutoRoutingService) validateProfile(ctx context.Context, profile *AutoRoutingProfile) error {
	if profile == nil || s.groupRepo == nil {
		return ErrAutoRoutingProfileInvalid
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" || len(profile.Name) > 100 ||
		(profile.Status != "" && profile.Status != AutoRoutingProfileStatusActive && profile.Status != AutoRoutingProfileStatusDisabled) {
		return ErrAutoRoutingProfileInvalid
	}
	if profile.MaxCandidates == 0 {
		profile.MaxCandidates = 10
	}
	if profile.MaxCandidates < 1 || profile.MaxCandidates > maxAutoRoutingProfileCandidates ||
		len(profile.Members) == 0 || len(profile.Members) > profile.MaxCandidates {
		return ErrAutoRoutingProfileInvalid
	}
	modelCatalogFilter := GroupModelAllowlist{}
	if profile.ModelCatalogFilter != nil {
		modelCatalogFilter = *profile.ModelCatalogFilter
	}
	normalizedModelCatalogFilter, err := normalizeGroupModelAllowlist(modelCatalogFilter)
	if err != nil {
		return err
	}
	profile.ModelCatalogFilter = &normalizedModelCatalogFilter
	members := append([]AutoRoutingProfileMember(nil), profile.Members...)
	sort.SliceStable(members, func(i, j int) bool {
		return members[i].SortOrder < members[j].SortOrder
	})
	seenGroups := make(map[int64]struct{}, len(members))
	for i := range members {
		groupID := members[i].GroupID
		if groupID <= 0 {
			return ErrAutoRoutingProfileInvalid
		}
		if _, exists := seenGroups[groupID]; exists {
			return ErrAutoRoutingProfileInvalid
		}
		seenGroups[groupID] = struct{}{}
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			return ErrAutoRoutingUnavailable.WithCause(err)
		}
		if group == nil || !group.IsActive() || group.Platform == PlatformComposite || !IsGroupContextValid(group) {
			return ErrAutoRoutingProfileInvalid
		}
		members[i].SortOrder = i
	}
	profile.Members = members
	if profile.Status == "" {
		profile.Status = AutoRoutingProfileStatusActive
	}
	return nil
}

func (s *AutoRoutingService) validateKeyRouting(ctx context.Context, userID int64, config *APIKeyAutoRoutingConfig) error {
	if config == nil || config.ProfileID <= 0 ||
		(config.Mode != "" && config.Mode != AutoRoutingModeAuto) {
		return ErrAutoRoutingProfileInvalid
	}
	profile, err := s.getProfile(ctx, config.ProfileID)
	if err != nil {
		return err
	}
	if profile.Status != AutoRoutingProfileStatusActive {
		return ErrAutoRoutingProfileInvalid
	}
	if config.CrossGroupRetry && !profile.CrossGroupRetryAllowed {
		return ErrAutoRoutingProfileInvalid
	}
	memberOrder := make(map[int64]int, len(profile.Members))
	for index, member := range profile.Members {
		memberOrder[member.GroupID] = index
	}
	availableGroups, err := s.apiKeys.GetAvailableGroups(ctx, userID)
	if err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	bindable := make(map[int64]struct{}, len(availableGroups))
	for i := range availableGroups {
		bindable[availableGroups[i].ID] = struct{}{}
	}
	switch config.SelectionMode {
	case AutoRoutingSelectionInherit:
		if len(config.GroupIDs) != 0 {
			return ErrAutoRoutingProfileInvalid
		}
	case AutoRoutingSelectionCustom:
		if len(config.GroupIDs) == 0 || len(config.GroupIDs) > profile.MaxCandidates {
			return ErrAutoRoutingProfileInvalid
		}
		seen := make(map[int64]struct{}, len(config.GroupIDs))
		lastOrder := -1
		for _, groupID := range config.GroupIDs {
			order, allowedByProfile := memberOrder[groupID]
			_, allowedByUser := bindable[groupID]
			if groupID <= 0 || !allowedByProfile || !allowedByUser {
				return ErrAutoRoutingProfileInvalid
			}
			if _, exists := seen[groupID]; exists {
				return ErrAutoRoutingProfileInvalid
			}
			seen[groupID] = struct{}{}
			if !profile.AllowCustomOrder && order <= lastOrder {
				return ErrAutoRoutingProfileInvalid
			}
			if order > lastOrder {
				lastOrder = order
			}
		}
	default:
		return ErrAutoRoutingProfileInvalid
	}
	return nil
}

func (s *AutoRoutingService) validateFixedGroup(ctx context.Context, userID int64, groupID *int64) error {
	if groupID == nil {
		return nil
	}
	groups, err := s.apiKeys.GetAvailableGroups(ctx, userID)
	if err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	for i := range groups {
		if groups[i].ID == *groupID {
			return nil
		}
	}
	return ErrGroupNotAllowed
}

type autoRoutingCandidate struct {
	group        *Group
	subscription *UserSubscription
}

func (s *AutoRoutingService) resolveCandidates(
	ctx context.Context,
	userID int64,
	profile *AutoRoutingProfile,
	config *APIKeyAutoRoutingConfig,
) ([]autoRoutingCandidate, *User, error) {
	user, err := s.apiKeys.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		if err == nil {
			err = errors.New("user not found")
		}
		return nil, nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if !user.IsActive() {
		return nil, nil, ErrUserNotActive
	}
	activeSubscriptions, err := s.apiKeys.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return nil, nil, ErrAutoRoutingUnavailable.WithCause(err)
	}
	subscribed := make(map[int64]bool, len(activeSubscriptions))
	subscriptionByGroup := make(map[int64]*UserSubscription, len(activeSubscriptions))
	for i := range activeSubscriptions {
		sub := activeSubscriptions[i]
		subscribed[sub.GroupID] = true
		subscriptionByGroup[sub.GroupID] = cloneUserSubscription(&sub)
	}
	members := make(map[int64]struct{}, len(profile.Members))
	for _, member := range profile.Members {
		members[member.GroupID] = struct{}{}
	}
	groupIDs := make([]int64, 0, len(profile.Members))
	if config.SelectionMode == AutoRoutingSelectionCustom {
		groupIDs = append(groupIDs, config.GroupIDs...)
	} else {
		for _, member := range profile.Members {
			groupIDs = append(groupIDs, member.GroupID)
		}
	}
	candidates := make([]autoRoutingCandidate, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		if _, ok := members[groupID]; !ok {
			continue
		}
		group, err := s.groupRepo.GetByID(ctx, groupID)
		if err != nil {
			if errors.Is(err, ErrGroupNotFound) {
				continue
			}
			return nil, nil, ErrAutoRoutingUnavailable.WithCause(err)
		}
		if group == nil || group.ID != groupID || !group.IsActive() ||
			group.Platform == PlatformComposite || !IsGroupContextValid(group) {
			continue
		}
		if !s.apiKeys.canUserBindGroupInternal(user, group, subscribed) {
			continue
		}
		var subscription *UserSubscription
		if group.IsSubscriptionType() {
			subscription = subscriptionByGroup[groupID]
			if subscription == nil {
				continue
			}
		}
		candidates = append(candidates, autoRoutingCandidate{
			group: group, subscription: subscription,
		})
	}
	if len(candidates) == 0 {
		return nil, nil, ErrAutoRoutingCandidateUnavailable
	}
	return candidates, user, nil
}

func filterAutoRoutingExecutionCandidates(
	candidates []autoRoutingCandidate,
	simpleMode bool,
	now time.Time,
) []autoRoutingCandidate {
	if simpleMode {
		return candidates
	}
	eligible := make([]autoRoutingCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.group == nil {
			continue
		}
		if !candidate.group.IsSubscriptionType() {
			eligible = append(eligible, candidate)
			continue
		}
		if autoRoutingSubscriptionWithinLimits(candidate.subscription, candidate.group, now) {
			eligible = append(eligible, candidate)
		}
	}
	return eligible
}

func autoRoutingSubscriptionWithinLimits(
	subscription *UserSubscription,
	group *Group,
	now time.Time,
) bool {
	if subscription == nil || group == nil {
		return false
	}

	validator := &SubscriptionService{now: func() time.Time { return now }}
	snapshot := cloneUserSubscription(subscription)
	_, err := validator.ValidateAndCheckLimits(snapshot, group)
	if err != nil {
		return false
	}
	return !(group.HasDailyLimit() && snapshot.DailyUsageUSD >= *group.DailyLimitUSD) &&
		!(group.HasWeeklyLimit() && snapshot.WeeklyUsageUSD >= *group.WeeklyLimitUSD) &&
		!(group.HasMonthlyLimit() && snapshot.MonthlyUsageUSD >= *group.MonthlyLimitUSD)
}

func (s *AutoRoutingService) resolveDirectory(
	ctx context.Context,
	candidates []autoRoutingCandidate,
	modelID string,
) ([]AutoModelCapability, error) {
	if s.catalog == nil {
		return nil, ErrAutoModelCatalogUnavailable
	}
	modelID = strings.TrimSpace(modelID)
	byModel := make(map[string]int)
	capabilities := make([]AutoModelCapability, 0)
	for _, candidate := range candidates {
		groupCapabilities, err := s.catalog.ListGroupModelCapabilities(ctx, candidate.group)
		if err != nil {
			return nil, ErrAutoRoutingUnavailable.WithCause(err)
		}
		for _, capability := range groupCapabilities {
			if modelID != "" && capability.ModelID != modelID {
				continue
			}
			if index, exists := byModel[capability.ModelID]; exists {
				capabilities[index] = mergeAutoModelCapability(capabilities[index], capability)
				continue
			}
			byModel[capability.ModelID] = len(capabilities)
			capabilities = append(capabilities, cloneAutoModelCapability(capability))
		}
	}
	return capabilities, nil
}

func filterAutoModelCapabilities(
	capabilities []AutoModelCapability,
	filter *GroupModelAllowlist,
) []AutoModelCapability {
	if filter == nil || !filter.Enabled {
		return capabilities
	}
	filtered := make([]AutoModelCapability, 0, len(capabilities))
	seen := make(map[string]struct{}, len(capabilities))
	for _, pattern := range filter.Models {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for _, capability := range capabilities {
			modelID := strings.TrimSpace(capability.ModelID)
			key := strings.ToLower(modelID)
			if modelID == "" || !groupAllowlistPatternMatches(pattern, modelID) {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			filtered = append(filtered, capability)
		}
	}
	return filtered
}

func validateAutoRoutingRequest(request AutoRoutingRequest) error {
	if request.IsModelDirectory || strings.TrimSpace(request.ModelID) == "" ||
		request.RequestID == "" || request.Body == nil || !autoModelEndpointSupported(request.Endpoint) {
		return ErrAutoRoutingRequestUnsupported
	}
	return nil
}

func autoRequestUsesImageGeneration(body []byte) bool {
	var payload struct {
		Tools []struct {
			Type string `json:"type"`
		} `json:"tools"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return true
	}
	for _, tool := range payload.Tools {
		if strings.EqualFold(strings.TrimSpace(tool.Type), "image_generation") {
			return true
		}
	}
	return false
}

func bindPreparedAutoRoutingKey(
	authenticated *APIKey,
	user *User,
	candidate autoRoutingCandidate,
	execution *AutoRoutingExecution,
) (*APIKey, error) {
	if authenticated == nil || user == nil || candidate.group == nil || execution == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	key := *authenticated
	key.RoutingMode = AutoRoutingModeAuto
	key.GroupID = int64Pointer(candidate.group.ID)
	key.Group = cloneAutoRoutingGroup(candidate.group)
	key.User = cloneAutoRoutingUser(user)
	key.User.UserGroupRPMOverride = nil
	bound, err := BindAutoRoutingExecution(&key, execution)
	if err != nil {
		return nil, err
	}
	return bound, nil
}

func candidateIDs(candidates []autoRoutingCandidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.group != nil {
			ids = append(ids, candidate.group.ID)
		}
	}
	return ids
}

func candidateByID(candidates []autoRoutingCandidate, groupID *int64) (autoRoutingCandidate, error) {
	if groupID == nil {
		return autoRoutingCandidate{}, ErrAutoRoutingUnavailable
	}
	for _, candidate := range candidates {
		if candidate.group != nil && candidate.group.ID == *groupID {
			return candidate, nil
		}
	}
	return autoRoutingCandidate{}, ErrAutoRoutingCandidateUnavailable
}

func cloneAutoRoutingProfile(profile *AutoRoutingProfile) AutoRoutingProfile {
	if profile == nil {
		return AutoRoutingProfile{}
	}
	cloned := *profile
	cloned.Members = append([]AutoRoutingProfileMember(nil), profile.Members...)
	cloned.ModelCatalogFilter = cloneAutoRoutingModelCatalogFilter(profile.ModelCatalogFilter)
	return cloned
}

func cloneAutoRoutingModelCatalogFilter(filter *GroupModelAllowlist) *GroupModelAllowlist {
	if filter == nil {
		return nil
	}
	cloned := *filter
	cloned.Models = append([]string(nil), filter.Models...)
	return &cloned
}

func cloneAPIKeyAutoRoutingConfig(config *APIKeyAutoRoutingConfig) *APIKeyAutoRoutingConfig {
	if config == nil {
		return nil
	}
	cloned := *config
	cloned.GroupIDs = append([]int64(nil), config.GroupIDs...)
	return &cloned
}

func cloneAutoModelCapability(capability AutoModelCapability) AutoModelCapability {
	capability.SourcePlatforms = append([]string(nil), capability.SourcePlatforms...)
	capability.UpstreamModels = append([]string(nil), capability.UpstreamModels...)
	capability.SupportedEndpoints = append([]AutoModelEndpoint(nil), capability.SupportedEndpoints...)
	return capability
}

func mergeAutoModelCapability(current, next AutoModelCapability) AutoModelCapability {
	current.SourcePlatforms = mergeAutoRoutingStrings(current.SourcePlatforms, next.SourcePlatforms)
	current.UpstreamModels = mergeAutoRoutingStrings(current.UpstreamModels, next.UpstreamModels)
	endpointSet := make(map[AutoModelEndpoint]struct{}, len(current.SupportedEndpoints)+len(next.SupportedEndpoints))
	for _, endpoint := range current.SupportedEndpoints {
		endpointSet[endpoint] = struct{}{}
	}
	for _, endpoint := range next.SupportedEndpoints {
		endpointSet[endpoint] = struct{}{}
	}
	current.SupportedEndpoints = current.SupportedEndpoints[:0]
	for endpoint := range endpointSet {
		current.SupportedEndpoints = append(current.SupportedEndpoints, endpoint)
	}
	sort.Slice(current.SupportedEndpoints, func(i, j int) bool {
		return current.SupportedEndpoints[i] < current.SupportedEndpoints[j]
	})
	current.AccountSchedulable = current.AccountSchedulable || next.AccountSchedulable
	return current
}

func mergeAutoRoutingStrings(left, right []string) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]string, 0, len(left)+len(right))
	for _, value := range append(append([]string(nil), left...), right...) {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	sort.Strings(merged)
	return merged
}

func cloneAutoRoutingGroup(group *Group) *Group {
	if group == nil {
		return nil
	}
	cloned := *group
	cloned.FallbackGroupID = nil
	cloned.FallbackGroupIDOnInvalidRequest = nil
	cloned.DailyLimitUSD = cloneFloat64Pointer(group.DailyLimitUSD)
	cloned.WeeklyLimitUSD = cloneFloat64Pointer(group.WeeklyLimitUSD)
	cloned.MonthlyLimitUSD = cloneFloat64Pointer(group.MonthlyLimitUSD)
	cloned.ImagePrice1K = cloneFloat64Pointer(group.ImagePrice1K)
	cloned.ImagePrice2K = cloneFloat64Pointer(group.ImagePrice2K)
	cloned.ImagePrice4K = cloneFloat64Pointer(group.ImagePrice4K)
	cloned.VideoModelPrices = make(map[string]map[string]float64, len(group.VideoModelPrices))
	for family, prices := range group.VideoModelPrices {
		copiedPrices := make(map[string]float64, len(prices))
		for resolution, price := range prices {
			copiedPrices[resolution] = price
		}
		cloned.VideoModelPrices[family] = copiedPrices
	}
	cloned.ModelPricing = append([]ChannelModelPricing(nil), group.ModelPricing...)
	cloned.ModelRouting = make(map[string][]int64, len(group.ModelRouting))
	for pattern, accountIDs := range group.ModelRouting {
		cloned.ModelRouting[pattern] = append([]int64(nil), accountIDs...)
	}
	cloned.SupportedModelScopes = append([]string(nil), group.SupportedModelScopes...)
	return &cloned
}

func cloneAutoRoutingAPIKey(key *APIKey) *APIKey {
	if key == nil {
		return nil
	}
	cloned := *key
	cloned.GroupID = cloneAutoRoutingInt64Pointer(key.GroupID)
	cloned.ExpiresAt = cloneTimePointer(key.ExpiresAt)
	cloned.LastUsedAt = cloneTimePointer(key.LastUsedAt)
	cloned.LastUsedIP = cloneStringPointer(key.LastUsedIP)
	cloned.Window5hStart = cloneTimePointer(key.Window5hStart)
	cloned.Window1dStart = cloneTimePointer(key.Window1dStart)
	cloned.Window7dStart = cloneTimePointer(key.Window7dStart)
	cloned.IPWhitelist = append([]string(nil), key.IPWhitelist...)
	cloned.IPBlacklist = append([]string(nil), key.IPBlacklist...)
	cloned.Group = cloneAutoRoutingGroup(key.Group)
	cloned.User = cloneAutoRoutingUser(key.User)
	if key.AutoRouting != nil {
		metadata := *key.AutoRouting
		if key.AutoRouting.RateMultiplier != nil {
			rateMultiplier := *key.AutoRouting.RateMultiplier
			metadata.RateMultiplier = &rateMultiplier
		}
		cloned.AutoRouting = &metadata
	}
	return &cloned
}

func cloneAutoRoutingUser(user *User) *User {
	if user == nil {
		return nil
	}
	cloned := *user
	cloned.AllowedGroups = append([]int64(nil), user.AllowedGroups...)
	cloned.GroupRates = make(map[int64]float64, len(user.GroupRates))
	for groupID, rate := range user.GroupRates {
		cloned.GroupRates[groupID] = rate
	}
	cloned.APIKeys = nil
	cloned.Subscriptions = make([]UserSubscription, len(user.Subscriptions))
	for i := range user.Subscriptions {
		subscription := cloneUserSubscription(&user.Subscriptions[i])
		if subscription != nil {
			cloned.Subscriptions[i] = *subscription
		}
	}
	return &cloned
}

func cloneUserSubscription(subscription *UserSubscription) *UserSubscription {
	if subscription == nil {
		return nil
	}
	cloned := *subscription
	cloned.DailyWindowStart = cloneTimePointer(subscription.DailyWindowStart)
	cloned.WeeklyWindowStart = cloneTimePointer(subscription.WeeklyWindowStart)
	cloned.MonthlyWindowStart = cloneTimePointer(subscription.MonthlyWindowStart)
	if subscription.Group != nil {
		cloned.Group = cloneAutoRoutingGroup(subscription.Group)
	}
	return &cloned
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAutoRoutingInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
