package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoRoutingUsageMetadataKeepsActualGroup(t *testing.T) {
	groupID := int64(15)
	key := &APIKey{GroupID: &groupID, AutoRouting: &AutoRoutingMetadata{
		ProfileID: 8, RoutingRevision: 3, AttemptNo: 1,
	}}
	usage := &UsageLog{GroupID: &groupID}
	applyAutoRoutingUsageMetadata(usage, key)
	require.Equal(t, "auto", usage.RoutingMode)
	require.Equal(t, int64(8), *usage.AutoProfileID)
	require.Equal(t, int64(15), *usage.GroupID)
	require.Equal(t, int64(3), usage.RoutingRevision)
	require.Equal(t, 1, usage.RouteAttemptCount)
	key.AutoRouting.ProfileID = 99
	require.Equal(t, int64(8), *usage.AutoProfileID)
}

func TestFixedUsageHasNoAutoMetadata(t *testing.T) {
	usage := &UsageLog{}
	applyAutoRoutingUsageMetadata(usage, &APIKey{})
	require.Equal(t, "fixed", usage.RoutingMode)
	require.Nil(t, usage.AutoProfileID)
}

func TestAutoGroupMediaPricingSnapshotIsNotReloaded(t *testing.T) {
	groupID := int64(15)
	key := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID}, AutoRouting: &AutoRoutingMetadata{ProfileID: 8}}
	var gateway *OpenAIGatewayService
	require.Same(t, key, gateway.apiKeyWithFreshGroupMediaPricing(context.Background(), key))
}
