package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAutoRoutingProfileWriteRequestAcceptsModelCatalogFilter(t *testing.T) {
	var request autoProfileWriteRequest
	err := json.Unmarshal([]byte(`{
		"name":"Auto",
		"revision":4,
		"model_catalog_filter":{"enabled":true,"models":["gpt-5.5-*"]}
	}`), &request)

	require.NoError(t, err)
	require.NotNil(t, request.ModelCatalogFilter)
	require.True(t, request.profile(12).ModelCatalogFilter.Enabled)
	require.Equal(t, []string{"gpt-5.5-*"}, request.profile(12).ModelCatalogFilter.Models)

	var legacyRequest autoProfileWriteRequest
	err = json.Unmarshal([]byte(`{"name":"Auto","revision":4}`), &legacyRequest)
	require.NoError(t, err)
	require.Nil(t, legacyRequest.profile(12).ModelCatalogFilter)
}

func TestAutoRoutingProfileResponseAlwaysIncludesFilter(t *testing.T) {
	profile := &service.AutoRoutingProfile{ID: 12, Name: "Auto"}
	content, err := json.Marshal(autoProfileResponse(profile))
	require.NoError(t, err)
	require.JSONEq(t, `{
		"id":12,
		"name":"Auto",
		"status":"",
		"revision":0,
		"max_candidates":0,
		"allow_custom_order":false,
		"cross_group_retry_allowed":false,
		"model_catalog_filter":{"enabled":false,"models":[]},
		"group_ids":[],
		"created_at":"0001-01-01T00:00:00Z",
		"updated_at":"0001-01-01T00:00:00Z"
	}`, string(content))
}
