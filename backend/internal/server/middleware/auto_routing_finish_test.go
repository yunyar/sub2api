package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type autoRoutingFinishRepository struct {
	service.AutoRoutingRepository
	released   int
	unknown    int
	releaseErr error
}

func (r *autoRoutingFinishRepository) ReleaseAutoRoutingDispatchClaim(context.Context, int64, int64) error {
	r.released++
	return r.releaseErr
}

func (r *autoRoutingFinishRepository) MarkAutoRoutingAttemptUnknown(context.Context, int64, int, string) (bool, error) {
	r.unknown++
	return true, nil
}

func TestAutoRoutingFinishDistinguishesClaimFromUpstreamDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name            string
		upstreamStarted bool
		releaseErr      error
		wantReleased    int
		wantUnknown     int
		wantStatus      string
	}{
		{name: "RPM denied before forward", wantReleased: 1, wantStatus: service.AutoRouteExecutionPrepared},
		{name: "snapshot failed before forward", wantReleased: 1, wantStatus: service.AutoRouteExecutionPrepared},
		{name: "upstream outcome unknown", upstreamStarted: true, wantUnknown: 1, wantStatus: service.AutoRouteExecutionUnknown},
		{name: "claim release storage failure", releaseErr: errors.New("storage unavailable"), wantReleased: 1, wantUnknown: 1, wantStatus: service.AutoRouteExecutionUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			groupID := int64(3)
			preparation := &service.AutoRoutingPreparation{Execution: &service.AutoRoutingExecution{
				ID: 7, ActualGroupID: &groupID, CurrentAttempt: 1, Status: service.AutoRouteExecutionDispatching,
			}}
			ctx.Set(autoRoutingStateContextKey, &autoRoutingRequestState{preparation: preparation, dispatched: true})
			if test.upstreamStarted {
				MarkAutoRoutingUpstreamStarted(ctx)
			}
			repo := &autoRoutingFinishRepository{releaseErr: test.releaseErr}
			routing := service.NewAutoRoutingService(nil, nil, repo, nil)
			FinishAutoRouting(ctx, nil, routing)
			FinishAutoRouting(ctx, nil, routing)
			require.Equal(t, test.wantReleased, repo.released)
			require.Equal(t, test.wantUnknown, repo.unknown)
			require.Equal(t, test.wantStatus, preparation.Execution.Status)
		})
	}
}
