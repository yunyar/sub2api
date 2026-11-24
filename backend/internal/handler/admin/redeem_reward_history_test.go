package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bloomRewardHistoryRepoStub struct {
	service.RedeemCodeRepository
}

func (bloomRewardHistoryRepoStub) ListBloomRewardHistory(_ context.Context, params pagination.PaginationParams, search string) ([]service.RedeemCode, *pagination.PaginationResult, error) {
	userID := int64(12)
	claimedAt := time.Date(2026, 11, 24, 14, 30, 0, 0, time.UTC)
	return []service.RedeemCode{{
		ID:                  -8,
		Type:                "bloom_reward",
		Value:               0.000001,
		Status:              service.StatusUsed,
		UsedBy:              &userID,
		UsedAt:               &claimedAt,
		CreatedAt:            claimedAt,
		ActivitySource:       "bloom",
		ActivityDescription: "easy",
		User:                 &service.User{ID: userID, Email: "alice@example.com"},
	}}, &pagination.PaginationResult{Total: 1, Page: params.Page, PageSize: params.PageSize}, nil
}

func TestGetRewardHistoryReturnsPaginatedClaimDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redeemService := service.NewRedeemService(bloomRewardHistoryRepoStub{}, nil, nil, nil, nil, nil, nil, nil)
	handler := NewRedeemHandler(nil, redeemService)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/v1/admin/redeem-codes/reward-history?page=2&page_size=50&search=alice", nil)
	handler.GetRewardHistory(ctx)

	require.Equal(t, 200, recorder.Code)
	var response struct {
		Data struct {
			Items []struct {
				Type                string  `json:"type"`
				Value               float64 `json:"value"`
				ActivitySource      string  `json:"activity_source"`
				ActivityDescription string  `json:"activity_description"`
				User                *struct {
					Email string `json:"email"`
				} `json:"user"`
			} `json:"items"`
			Total int `json:"total"`
			Page  int `json:"page"`
			Size  int `json:"page_size"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 1, response.Data.Total)
	require.Equal(t, 2, response.Data.Page)
	require.Equal(t, 50, response.Data.Size)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, "bloom_reward", response.Data.Items[0].Type)
	require.Equal(t, "bloom", response.Data.Items[0].ActivitySource)
	require.Equal(t, "easy", response.Data.Items[0].ActivityDescription)
	require.Equal(t, "alice@example.com", response.Data.Items[0].User.Email)
	require.InDelta(t, 0.000001, response.Data.Items[0].Value, 1e-12)
}
