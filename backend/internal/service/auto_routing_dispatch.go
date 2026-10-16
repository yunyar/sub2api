package service

import (
	"context"
	"encoding/json"
	"time"
)

type autoRoutingDispatchRecorder interface {
	RecordAutoRoutingDispatchSnapshot(context.Context, int64, int, int64, int64, string, []byte) error
}

type autoRoutingDispatchReleaser interface {
	ReleaseAutoRoutingDispatchClaim(context.Context, int64, int64) error
}

func (s *AutoRoutingService) ReleaseDispatchClaim(ctx context.Context, preparation *AutoRoutingPreparation) error {
	if s == nil || preparation == nil || preparation.Execution == nil || preparation.Execution.ActualGroupID == nil || preparation.Execution.Status != AutoRouteExecutionDispatching {
		return ErrAutoRoutingExecutionInvalid
	}
	releaser, ok := s.repo.(autoRoutingDispatchReleaser)
	if !ok {
		return ErrAutoRoutingUnavailable
	}
	if err := releaser.ReleaseAutoRoutingDispatchClaim(ctx, preparation.Execution.ID, *preparation.Execution.ActualGroupID); err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	preparation.Execution.Status = AutoRouteExecutionPrepared
	preparation.Execution.CurrentAttempt = 0
	return nil
}

func (s *AutoRoutingService) RecordDispatchAccount(ctx context.Context, key *APIKey, account *Account) error {
	if key == nil || key.AutoRouting == nil || key.GroupID == nil || key.Group == nil || account == nil || account.ID <= 0 {
		return ErrAutoRoutingExecutionInvalid
	}
	recorder, ok := s.repo.(autoRoutingDispatchRecorder)
	if !ok {
		return ErrAutoRoutingUnavailable
	}
	snapshot, err := json.Marshal(struct {
		Version     int       `json:"version"`
		Group       *Group    `json:"group"`
		AccountRate float64   `json:"account_rate"`
		UserRate    *float64  `json:"user_rate"`
		PricingAt   time.Time `json:"pricing_at"`
	}{1, key.Group, account.BillingRateMultiplier(), key.AutoRouting.RateMultiplier, key.AutoRouting.PricingAt})
	if err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	err = recorder.RecordAutoRoutingDispatchSnapshot(ctx, key.AutoRouting.ExecutionID,
		key.AutoRouting.AttemptNo, *key.GroupID, account.ID, account.Platform, snapshot)
	if err != nil {
		return ErrAutoRoutingUnavailable.WithCause(err)
	}
	return nil
}
