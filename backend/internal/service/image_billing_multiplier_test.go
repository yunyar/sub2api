package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageRateAlwaysUsesGroupImageMultiplier(t *testing.T) {
	for _, legacyIndependent := range []bool{false, true} {
		for _, multiplier := range []float64{0, 0.5, 1.8, -1} {
			key := &APIKey{Group: &Group{RateMultiplier: 3, ImageRateIndependent: legacyIndependent, ImageRateMultiplier: multiplier}}
			require.Equal(t, math.Max(multiplier, 0), resolveImageRateMultiplier(key, 9))
		}
	}
	require.Equal(t, 1.0, resolveImageRateMultiplier(nil, 1))
}
