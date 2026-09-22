package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type playgroundImageBillingRepoStub struct {
	UsageBillingRepository
	mu       sync.Mutex
	balance  float64
	holds    map[string]float64
	reserves int
}

func (r *playgroundImageBillingRepoStub) ReserveBatchImageBalance(_ context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.holds == nil {
		r.holds = map[string]float64{}
	}
	if _, ok := r.holds[cmd.BatchID]; ok {
		return &BatchImageBalanceHoldResult{Applied: false}, nil
	}
	if r.balance < cmd.HoldAmount {
		return nil, ErrBatchImageInsufficientBalance
	}
	r.balance -= cmd.HoldAmount
	r.holds[cmd.BatchID] = cmd.HoldAmount
	r.reserves++
	return &BatchImageBalanceHoldResult{Applied: true}, nil
}

func (r *playgroundImageBillingRepoStub) ReleaseBatchImageBalance(_ context.Context, cmd *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if amount, ok := r.holds[cmd.BatchID]; ok {
		r.balance += amount
		delete(r.holds, cmd.BatchID)
	}
	return &BatchImageBalanceHoldResult{Applied: true}, nil
}

func TestReservePlaygroundImageBalance_ConcurrentInsufficientBalanceAndMultiplier(t *testing.T) {
	price := 2.0
	repo := &playgroundImageBillingRepoStub{balance: 5}
	svc := &OpenAIGatewayService{usageBillingRepo: repo, billingService: NewBillingService(nil, nil)}
	apiKey := &APIKey{ID: 9, User: &User{ID: 7}, GroupID: int64Ptr(3), Group: &Group{ID: 3, ImagePrice2K: &price, ImageRateMultiplier: 1.5}}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, id)
			_, err := svc.ReservePlaygroundImageBalance(ctx, apiKey, "gpt-image-1", "2K", 1, id)
			if err == nil {
				successes.Add(1)
			}
		}(id)
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
	require.InDelta(t, 2, repo.balance, 1e-9)
}

func TestReservePlaygroundImageBalance_DuplicateRequestAndZeroPrice(t *testing.T) {
	zero := 0.0
	repo := &playgroundImageBillingRepoStub{balance: 1}
	svc := &OpenAIGatewayService{usageBillingRepo: repo, billingService: NewBillingService(nil, nil)}
	apiKey := &APIKey{ID: 9, User: &User{ID: 7}, GroupID: int64Ptr(3), Group: &Group{ID: 3, ImagePrice2K: &zero, ImageRateMultiplier: 1}}
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "same")
	hold, err := svc.ReservePlaygroundImageBalance(ctx, apiKey, "gpt-image-1", "2K", 3, "payload")
	require.NoError(t, err)
	require.NotNil(t, hold)
	require.Zero(t, hold.HoldAmount)
	_, err = svc.ReservePlaygroundImageBalance(ctx, apiKey, "gpt-image-1", "2K", 3, "payload")
	require.ErrorIs(t, err, ErrPlaygroundImageHoldConflict)
	require.Equal(t, 1, repo.reserves)
}

func TestReservePlaygroundImageBalance_SimpleModeSkipsHold(t *testing.T) {
	repo := &playgroundImageBillingRepoStub{balance: 1}
	svc := &OpenAIGatewayService{cfg: &config.Config{RunMode: config.RunModeSimple}, usageBillingRepo: repo, billingService: NewBillingService(nil, nil)}
	price := 1.0
	apiKey := &APIKey{ID: 9, User: &User{ID: 7}, Group: &Group{ID: 3, ImagePrice2K: &price}}
	hold, err := svc.ReservePlaygroundImageBalance(context.Background(), apiKey, "gpt-image-1", "2K", 1, "payload")
	require.NoError(t, err)
	require.Nil(t, hold)
	require.Zero(t, repo.reserves)
}
