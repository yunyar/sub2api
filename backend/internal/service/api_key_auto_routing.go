package service

func (s *APIKeyService) SetAutoRoutingService(routing *AutoRoutingService) {
	s.autoRouting = routing
}

func (s *APIKeyService) AutoRoutingService() *AutoRoutingService {
	if s == nil {
		return nil
	}
	return s.autoRouting
}

func applyAutoRoutingUsageMetadata(usage *UsageLog, key *APIKey) {
	usage.RoutingMode = "fixed"
	if key == nil || key.AutoRouting == nil {
		return
	}
	metadata := *key.AutoRouting
	usage.RoutingMode = "auto"
	usage.AutoProfileID = &metadata.ProfileID
	usage.RoutingRevision = metadata.RoutingRevision
	usage.RouteAttemptCount = metadata.AttemptNo
}
