package service

import (
	"context"
	"fmt"
)

func (s *AutoRoutingService) ResolvePlaygroundAutoKey(ctx context.Context, userID, profileID int64) (*APIKey, error) {
	if _, err := s.playgroundProfile(ctx, userID, profileID); err != nil {
		return nil, err
	}
	key, _, exists, err := s.loadPlaygroundAutoKey(ctx, userID, profileID)
	if err != nil || exists {
		return key, err
	}

	config := &APIKeyAutoRoutingConfig{
		Mode:          AutoRoutingModeAuto,
		ProfileID:     profileID,
		SelectionMode: AutoRoutingSelectionInherit,
		GroupIDs:      []int64{},
	}
	key, _, _, err = s.createPlaygroundAutoKey(ctx, userID, profileID, config)
	return key, err
}

func (s *AutoRoutingService) GetPlaygroundAutoRouting(ctx context.Context, userID, profileID int64) (*APIKeyAutoRoutingConfig, error) {
	if _, err := s.playgroundProfile(ctx, userID, profileID); err != nil {
		return nil, err
	}
	_, config, exists, err := s.loadPlaygroundAutoKey(ctx, userID, profileID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &APIKeyAutoRoutingConfig{
			Mode:          AutoRoutingModeAuto,
			ProfileID:     profileID,
			SelectionMode: AutoRoutingSelectionInherit,
			GroupIDs:      []int64{},
		}, nil
	}
	return config, nil
}

func (s *AutoRoutingService) UpdatePlaygroundAutoRouting(
	ctx context.Context,
	userID, profileID int64,
	config *APIKeyAutoRoutingConfig,
	expectedRevision int64,
) (*APIKeyAutoRoutingConfig, error) {
	if config == nil || config.CrossGroupRetry || expectedRevision < 0 {
		return nil, ErrAutoRoutingProfileInvalid
	}
	if _, err := s.playgroundProfile(ctx, userID, profileID); err != nil {
		return nil, err
	}

	updated := cloneAPIKeyAutoRoutingConfig(config)
	updated.Mode = AutoRoutingModeAuto
	updated.ProfileID = profileID
	updated.CrossGroupRetry = false
	updated.Revision = expectedRevision
	if updated.SelectionMode == AutoRoutingSelectionInherit {
		updated.GroupIDs = nil
	}
	if err := s.validateKeyRouting(ctx, userID, updated); err != nil {
		return nil, err
	}

	_, _, exists, err := s.loadPlaygroundAutoKey(ctx, userID, profileID)
	if err != nil {
		return nil, err
	}
	if !exists {
		key, config, created, err := s.createPlaygroundAutoKey(ctx, userID, profileID, updated)
		if err != nil {
			return nil, err
		}
		if created {
			return config, nil
		}
		updated.APIKeyID = key.ID
	}

	key, _, exists, err := s.loadPlaygroundAutoKey(ctx, userID, profileID)
	if err != nil {
		return nil, err
	}
	if !exists || key == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	result, err := s.UpdateKeyRouting(ctx, userID, key.ID, updated, nil, expectedRevision)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *AutoRoutingService) playgroundProfile(ctx context.Context, userID, profileID int64) (*AutoRoutingProfile, error) {
	if userID <= 0 || profileID <= 0 {
		return nil, ErrAutoRoutingProfileNotFound
	}
	profile, err := s.getProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	if profile.Status != AutoRoutingProfileStatusActive {
		return nil, ErrAutoRoutingProfileNotFound
	}
	available, err := s.AvailableProfiles(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range available {
		if available[i].ID == profileID {
			return profile, nil
		}
	}
	return nil, ErrAutoRoutingProfileNotFound
}

func (s *AutoRoutingService) loadPlaygroundAutoKey(
	ctx context.Context,
	userID, profileID int64,
) (*APIKey, *APIKeyAutoRoutingConfig, bool, error) {
	if s == nil || s.repo == nil || s.apiKeys == nil {
		return nil, nil, false, ErrAutoRoutingUnavailable
	}
	binding, err := s.repo.GetPlaygroundAutoKeyBinding(ctx, userID, profileID)
	if err != nil {
		return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if binding == nil {
		return nil, nil, false, nil
	}
	if binding.UserID != userID || binding.ProfileID != profileID || binding.APIKeyID <= 0 {
		return nil, nil, false, ErrAutoRoutingUnavailable
	}
	key, err := s.apiKeys.GetByID(ctx, binding.APIKeyID)
	if err != nil {
		return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(err)
	}
	if key == nil || key.UserID != userID || key.RoutingMode != AutoRoutingModeAuto ||
		key.Status != StatusAPIKeyActive || key.IsExpired() || key.IsQuotaExhausted() {
		return nil, nil, false, ErrAutoRoutingUnavailable
	}
	config, err := s.getAPIKeyRouting(ctx, key.ID)
	if err != nil {
		return nil, nil, false, err
	}
	if config == nil || config.Mode != AutoRoutingModeAuto || config.ProfileID != profileID ||
		config.Revision != key.RoutingRevision {
		return nil, nil, false, ErrAutoRoutingUnavailable
	}
	return key, config, true, nil
}

func (s *AutoRoutingService) createPlaygroundAutoKey(
	ctx context.Context,
	userID, profileID int64,
	config *APIKeyAutoRoutingConfig,
) (*APIKey, *APIKeyAutoRoutingConfig, bool, error) {
	candidate, err := s.apiKeys.Create(ctx, userID, CreateAPIKeyRequest{
		Name:    "Playground Auto",
		GroupID: nil,
	})
	if err != nil {
		key, stored, exists, loadErr := s.loadPlaygroundAutoKey(ctx, userID, profileID)
		if loadErr != nil {
			return nil, nil, false, loadErr
		}
		if exists {
			return key, stored, false, nil
		}
		return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(err)
	}

	createdConfig := cloneAPIKeyAutoRoutingConfig(config)
	createdConfig.APIKeyID = candidate.ID
	createdConfig.Mode = AutoRoutingModeAuto
	createdConfig.ProfileID = profileID
	createdConfig.CrossGroupRetry = false
	createdConfig.Revision = candidate.RoutingRevision
	if createdConfig.SelectionMode == AutoRoutingSelectionInherit {
		createdConfig.GroupIDs = nil
	}
	if _, err := s.UpdateKeyRouting(ctx, userID, candidate.ID, createdConfig, nil, candidate.RoutingRevision); err != nil {
		if deleteErr := s.apiKeys.Delete(ctx, candidate.ID, userID); deleteErr != nil {
			return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(fmt.Errorf("configure new playground Auto key: %v; deactivate candidate: %w", err, deleteErr))
		}
		key, stored, exists, loadErr := s.loadPlaygroundAutoKey(ctx, userID, profileID)
		if loadErr != nil {
			return nil, nil, false, loadErr
		}
		if exists {
			return key, stored, false, nil
		}
		return nil, nil, false, err
	}

	upsertErr := s.repo.UpsertPlaygroundAutoKeyBinding(ctx, &PlaygroundAutoKeyBinding{
		UserID: userID, ProfileID: profileID, APIKeyID: candidate.ID,
	})
	binding, readErr := s.repo.GetPlaygroundAutoKeyBinding(ctx, userID, profileID)
	if readErr != nil {
		return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(readErr)
	}
	if binding != nil && binding.APIKeyID == candidate.ID {
		key, stored, exists, loadErr := s.loadPlaygroundAutoKey(ctx, userID, profileID)
		if loadErr != nil {
			return nil, nil, false, loadErr
		}
		if exists {
			return key, stored, true, nil
		}
	}

	if deleteErr := s.apiKeys.Delete(ctx, candidate.ID, userID); deleteErr != nil {
		return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(deleteErr)
	}
	if binding != nil {
		key, stored, exists, loadErr := s.loadPlaygroundAutoKey(ctx, userID, profileID)
		if loadErr != nil {
			return nil, nil, false, loadErr
		}
		if exists {
			return key, stored, false, nil
		}
	}
	if upsertErr != nil {
		return nil, nil, false, ErrAutoRoutingUnavailable.WithCause(upsertErr)
	}
	return nil, nil, false, ErrAutoRoutingUnavailable
}
