package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestReserveImageInflightBalance_SkipsWhenPlaygroundHoldExists(t *testing.T) {
	cache := newHandlerInflightCache(1)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	estimator := &countingEstimator{cost: 0.9, priced: true}
	apiKey := &service.APIKey{User: &service.User{ID: 1}}
	hold := &service.BatchImageBalanceHoldCommand{BatchID: "playground-image:1:request"}

	done, err := reserveImageInflightBalance(
		newInflightTestGinContext(),
		billing,
		estimator,
		apiKey,
		nil,
		service.InflightEstimateRequest{Model: "image-model", Kind: service.InflightEstimateImage, Units: 1},
		hold,
	)

	require.NoError(t, err)
	require.NotNil(t, done)
	done()
	require.Zero(t, estimator.calls, "Playground's database hold must bypass Redis estimation")
	require.Zero(t, cache.count(), "Playground's database hold must not create a Redis reservation")
}

func TestReserveImageInflightBalance_ReservesAndReleasesOrdinaryImageRequest(t *testing.T) {
	cache := newHandlerInflightCache(1)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	estimator := &countingEstimator{cost: 0.9, priced: true}
	apiKey := &service.APIKey{User: &service.User{ID: 2}}

	done, err := reserveImageInflightBalance(
		newInflightTestGinContext(),
		billing,
		estimator,
		apiKey,
		nil,
		service.InflightEstimateRequest{Model: "image-model", Kind: service.InflightEstimateImage, Units: 2},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, 1, estimator.calls)
	require.Equal(t, 1, cache.count())
	done()
	require.Zero(t, cache.count(), "done must release the ordinary image reservation")
}

func TestReserveImageInflightBalance_RejectsWhenExistingReservationLeavesInsufficientBalance(t *testing.T) {
	cache := newHandlerInflightCache(1.5)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	estimator := &countingEstimator{cost: 0.9, priced: true}
	apiKey := &service.APIKey{User: &service.User{ID: 3}}

	held, err := billing.ReserveInflight(context.Background(), apiKey.User, nil, nil, 1)
	require.NoError(t, err)
	require.NotNil(t, held)
	defer held.HandlerDone()

	done, err := reserveImageInflightBalance(
		newInflightTestGinContext(),
		billing,
		estimator,
		apiKey,
		nil,
		service.InflightEstimateRequest{Model: "image-model", Kind: service.InflightEstimateImage, Units: 1},
		nil,
	)

	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.Equal(t, 1, cache.count(), "a rejected image request must not add a reservation")
	require.NotNil(t, done)
	done()
	held.HandlerDone()
	require.Zero(t, cache.count(), "the pre-existing reservation must release normally")
}

func TestReserveImageInflightBalance_RespectsDisabledSimpleAndSubscriptionModes(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		cfg := &config.Config{}
		billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(billing.Stop)
		estimator := &countingEstimator{cost: 1, priced: true}

		done, err := reserveImageInflightBalance(
			newInflightTestGinContext(),
			billing,
			estimator,
			&service.APIKey{User: &service.User{ID: 4}},
			nil,
			service.InflightEstimateRequest{Model: "image-model", Kind: service.InflightEstimateImage, Units: 1},
			nil,
		)

		require.NoError(t, err)
		done()
		require.Zero(t, estimator.calls)
	})

	t.Run("simple mode", func(t *testing.T) {
		cfg := &config.Config{RunMode: config.RunModeSimple}
		cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
		billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(billing.Stop)
		estimator := &countingEstimator{cost: 1, priced: true}

		done, err := reserveImageInflightBalance(
			newInflightTestGinContext(),
			billing,
			estimator,
			&service.APIKey{User: &service.User{ID: 5}},
			nil,
			service.InflightEstimateRequest{Model: "image-model", Kind: service.InflightEstimateImage, Units: 1},
			nil,
		)

		require.NoError(t, err)
		done()
		require.Zero(t, estimator.calls)
	})

	t.Run("subscription", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
		billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(billing.Stop)
		estimator := &countingEstimator{cost: 1, priced: true}
		apiKey := &service.APIKey{
			User:  &service.User{ID: 6},
			Group: &service.Group{SubscriptionType: service.SubscriptionTypeSubscription},
		}

		done, err := reserveImageInflightBalance(
			newInflightTestGinContext(),
			billing,
			estimator,
			apiKey,
			&service.UserSubscription{},
			service.InflightEstimateRequest{Model: "image-model", Kind: service.InflightEstimateImage, Units: 1},
			nil,
		)

		require.NoError(t, err)
		done()
		require.Zero(t, estimator.calls)
	})
}
