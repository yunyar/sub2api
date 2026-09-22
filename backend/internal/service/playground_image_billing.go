package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrPlaygroundImageHoldConflict = errors.New("playground image balance hold already exists")

func PlaygroundImageTerminalRequestID(batchID string) string {
	return "playground_image_terminal:" + strings.TrimSpace(batchID)
}

func (s *OpenAIGatewayService) ReservePlaygroundImageBalance(ctx context.Context, apiKey *APIKey, model, imageSize string, count int, payloadHash string) (*BatchImageBalanceHoldCommand, error) {
	if s != nil && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, nil
	}
	if s == nil || s.usageBillingRepo == nil || s.billingService == nil || apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return nil, ErrBatchImageBillingHoldFailed.WithCause(errors.New("playground image billing is not configured"))
	}
	apiKey = s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey)
	if apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return nil, ErrBatchImageBillingHoldFailed.WithCause(errors.New("playground image pricing is unavailable"))
	}
	if count <= 0 {
		count = 1
	}
	base := 1.0
	if s.cfg != nil {
		base = s.cfg.Default.RateMultiplier
	}
	if apiKey.GroupID != nil {
		base = s.ResolveUserGroupRateMultiplier(ctx, apiKey.User.ID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	}
	_, imageMultiplier := computePeakAwareMultipliers(apiKey, base, time.Now())
	cost := s.calculateOpenAIImageCost(ctx, model, apiKey, &OpenAIForwardResult{
		Model: model, ImageCount: count, ImageSize: NormalizeImageBillingTierOrDefault(imageSize),
	}, imageMultiplier)
	if cost == nil || math.IsNaN(cost.ActualCost) || math.IsInf(cost.ActualCost, 0) || cost.ActualCost < 0 {
		return nil, ErrBatchImageBillingHoldFailed.WithCause(errors.New("playground image price cannot be safely estimated"))
	}
	amount := QuantizeUsageBillingAmount(cost.ActualCost)
	requestID := resolveUsageBillingRequestID(ctx, "")
	batchID := fmt.Sprintf("playground-image:%d:%s", apiKey.ID, strings.TrimSpace(requestID))
	hold := &BatchImageBalanceHoldCommand{
		RequestID: BatchImageHoldRequestID(batchID), APIKeyID: apiKey.ID, UserID: apiKey.User.ID,
		BatchID: batchID, HoldAmount: amount, UnitAmount: QuantizeUsageBillingAmount(amount / float64(count)), RequestedCount: count, RequestPayloadHash: payloadHash,
	}
	reserved, err := s.usageBillingRepo.ReserveBatchImageBalance(ctx, hold)
	if err != nil {
		if errors.Is(err, ErrBatchImageInsufficientBalance) {
			return nil, ErrBatchImageInsufficientBalance
		}
		return nil, ErrBatchImageBillingHoldFailed.WithCause(fmt.Errorf("reserve playground image balance: %w", err))
	}
	if reserved == nil || !reserved.Applied {
		return nil, ErrPlaygroundImageHoldConflict
	}
	return hold, nil
}

func (s *OpenAIGatewayService) ReleasePlaygroundImageBalance(ctx context.Context, hold *BatchImageBalanceHoldCommand) error {
	if hold == nil {
		return nil
	}
	if s == nil || s.usageBillingRepo == nil {
		return ErrBatchImageBillingHoldFailed
	}
	release := *hold
	release.RequestID = PlaygroundImageTerminalRequestID(hold.BatchID)
	if _, err := s.usageBillingRepo.ReleaseBatchImageBalance(ctx, &release); err != nil {
		return ErrBatchImageBillingHoldFailed.WithCause(err)
	}
	return nil
}
