package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type autoRoutingRPMCache interface {
	AdmitAutoRoutingRPM(context.Context, int64, int64, int64, int, int) (int, error)
}

func (s *AutoRoutingService) SetDispatchBilling(billing *BillingCacheService) {
	s.dispatchBilling = billing
}

func (s *AutoRoutingService) AdmitDispatch(ctx context.Context, key *APIKey) error {
	if s == nil || s.dispatchBilling == nil || key == nil || key.AutoRouting == nil || key.User == nil || key.Group == nil {
		return ErrAutoRoutingUnavailable
	}
	if s.dispatchBilling.cfg != nil && s.dispatchBilling.cfg.RunMode == config.RunModeSimple {
		return nil
	}
	return s.dispatchBilling.checkAutoRoutingRPM(ctx, key.User, key, key.Group)
}

func (s *BillingCacheService) checkAutoRoutingRPM(ctx context.Context, user *User, key *APIKey, group *Group) error {
	cache, supported := s.userRPMCache.(autoRoutingRPMCache)
	if !supported {
		return s.checkRPM(ctx, user, group)
	}
	if user == nil || key == nil || key.AutoRouting == nil || group == nil {
		return ErrBillingServiceUnavailable
	}
	groupLimit := group.RPMLimit
	if user.UserGroupRPMOverride != nil {
		groupLimit = *user.UserGroupRPMOverride
	}
	result, err := cache.AdmitAutoRoutingRPM(ctx, user.ID, group.ID, key.AutoRouting.ExecutionID, user.RPMLimit, groupLimit)
	if err != nil {
		logger.LegacyPrintf("service.billing_cache", "Warning: Auto RPM admission failed for user=%d group=%d: %v", user.ID, group.ID, err)
		return nil
	}
	switch result {
	case 1:
		return ErrGroupRPMExceeded
	case 2:
		return ErrUserRPMExceeded
	}
	return nil
}
