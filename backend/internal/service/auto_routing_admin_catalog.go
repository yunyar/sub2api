package service

import "context"

func (s *AutoRoutingService) PreviewModelCatalog(ctx context.Context, groupIDs []int64) ([]AutoModelCapability, error) {
	if s == nil || s.groupRepo == nil || s.catalog == nil {
		return nil, ErrAutoRoutingUnavailable
	}
	if len(groupIDs) > maxAutoRoutingProfileCandidates {
		return nil, ErrAutoRoutingProfileInvalid
	}
	seenGroups := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			return nil, ErrAutoRoutingProfileInvalid
		}
		if _, exists := seenGroups[groupID]; exists {
			return nil, ErrAutoRoutingProfileInvalid
		}
		seenGroups[groupID] = struct{}{}
	}
	capabilities := make([]AutoModelCapability, 0)
	byModel := make(map[string]int)
	for _, groupID := range groupIDs {
		group, err := s.groupRepo.GetByIDLite(ctx, groupID)
		if err != nil {
			return nil, err
		}
		if err := validateAutoModelCatalogGroup(group); err != nil {
			return nil, ErrAutoRoutingProfileInvalid
		}
		models, err := s.catalog.ListGroupModelCapabilities(ctx, group)
		if err != nil {
			return nil, ErrAutoRoutingUnavailable.WithCause(err)
		}
		for _, capability := range models {
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
