package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAutoRPMConcurrentReplayCountsOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &userRPMCacheImpl{rdb: client}
	var workers sync.WaitGroup
	results := make(chan int, 16)
	errors := make(chan error, 16)
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := cache.AdmitAutoRoutingRPM(context.Background(), 1, 3, 9, 1, 1)
			results <- result
			errors <- err
		}()
	}
	workers.Wait()
	for range 16 {
		require.NoError(t, <-errors)
		require.Zero(t, <-results)
	}
	userCount, err := cache.GetUserRPM(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, userCount)
	groupCount, err := cache.GetUserGroupRPM(context.Background(), 1, 3)
	require.NoError(t, err)
	require.Equal(t, 1, groupCount)
	denied, err := cache.AdmitAutoRoutingRPM(context.Background(), 1, 4, 10, 1, 0)
	require.NoError(t, err)
	require.Equal(t, 2, denied)
}

func TestAutoRPMGroupDenialDoesNotConsumeGlobalBudget(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &userRPMCacheImpl{rdb: client}
	result, err := cache.AdmitAutoRoutingRPM(context.Background(), 1, 3, 1, 2, 1)
	require.NoError(t, err)
	require.Zero(t, result)
	result, err = cache.AdmitAutoRoutingRPM(context.Background(), 1, 3, 2, 2, 1)
	require.NoError(t, err)
	require.Equal(t, 1, result)
	result, err = cache.AdmitAutoRoutingRPM(context.Background(), 1, 4, 3, 2, 0)
	require.NoError(t, err)
	require.Zero(t, result)
	count, err := cache.GetUserRPM(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 2, count)
}
