package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlaygroundImageHoldDoesNotDeductBalanceTwice(t *testing.T) {
	parameters := &postUsageBillingParams{
		Cost:                &CostBreakdown{TotalCost: 2, ActualCost: 3},
		APIKey:              &APIKey{ID: 4, Quota: 10},
		User:                &User{ID: 5},
		Account:             &Account{ID: 6, Type: AccountTypeAPIKey},
		PlaygroundImageHold: &BatchImageBalanceHoldCommand{HoldAmount: 3},
		APIKeyService:       &APIKeyService{},
	}
	command := buildUsageBillingCommand("held-image", &UsageLog{ImageCount: 3}, parameters)
	require.NotNil(t, command)
	require.Zero(t, command.BalanceCost)
	require.Equal(t, 3.0, command.APIKeyQuotaCost)
	require.Equal(t, 3, command.ImageCount)
	parameters.PlaygroundImageHold = nil
	ordinary := buildUsageBillingCommand("ordinary-image", nil, parameters)
	require.Equal(t, 3.0, ordinary.BalanceCost)
}
