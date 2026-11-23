package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPlaygroundSearchStream(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		request    bool
		blockStage string
		enabled    bool
		fail       bool
	}{
		{name: "direct answer", enabled: true},
		{name: "one search and one reply", request: true, enabled: true},
		{name: "query blocked before search", request: true, enabled: true, blockStage: "playground_search_query"},
		{name: "results blocked before followup", request: true, enabled: true, blockStage: "playground_search_results"},
		{name: "tool disabled"},
		{name: "followup failure", request: true, enabled: true, fail: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/playground/chat/completions", nil)
			body := []byte(`{"model":"test","stream":true,"messages":[{"role":"user","content":"question"}]}`)
			var auditStages []string
			searchCalls, forwards, reserved := 0, 0, 0
			options := playgroundSearchOptions{
				enabled: scenario.enabled,
				audit: func(stage, text string) error {
					auditStages = append(auditStages, stage)
					require.NotEmpty(t, text)
					if stage == scenario.blockStage {
						return errors.New("Blocked by group audit")
					}
					return nil
				},
				search: func(_ context.Context, query string) ([]websearch.SearchResult, error) {
					searchCalls++
					require.Equal(t, "public news", query)
					return []websearch.SearchResult{{URL: "https://example.com", Title: "Public news"}}, nil
				},
				reserve: func(_ []byte) error {
					reserved++
					return nil
				},
			}
			first, followup, err := forwardPlaygroundChat(c, body, options, func(payload []byte) (*int, error) {
				forwards++
				c.Header("Content-Type", "text/event-stream")
				searchCount := 0
				if forwards == 1 {
					require.Equal(t, scenario.enabled, gjson.GetBytes(payload, "tools.0.function.name").String() == playgroundSearchTool)
					if scenario.request {
						_, _ = fmt.Fprint(c.Writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_test\",\"function\":{\"name\":\"playground_web_lookup\",\"arguments\":\"{\\\"query\\\":\\\"public\"}}]}}]}\r\n\r\n")
						_, _ = fmt.Fprint(c.Writer, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\" news\\\"}\"}}]}}]}\n\n")
						_, _ = fmt.Fprint(c.Writer, "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n")
						_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
						return &searchCount, nil
					}
				} else {
					require.Equal(t, "none", gjson.GetBytes(payload, "tool_choice").String())
					require.Equal(t, "call_test", gjson.GetBytes(payload, "messages.2.tool_call_id").String())
					require.Contains(t, gjson.GetBytes(payload, "messages.2.content").String(), "https://example.com")
					if scenario.fail {
						c.JSON(http.StatusBadGateway, gin.H{"private": "upstream error detail"})
						return nil, errors.New("followup failed")
					}
				}
				_, _ = fmt.Fprint(c.Writer, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n")
				_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
				return &searchCount, nil
			}, func(result *int) { *result += 1 })

			require.NoError(t, err)
			require.NotNil(t, first)
			require.NotContains(t, recorder.Body.String(), "tool_calls")
			if scenario.fail || scenario.blockStage != "" {
				require.Nil(t, followup)
				require.Contains(t, recorder.Body.String(), "playground_search_error")
				require.NotContains(t, recorder.Body.String(), "upstream error detail")
			} else {
				require.Contains(t, recorder.Body.String(), `"content":"answer"`)
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "[DONE]"))
			}
			switch {
			case !scenario.request:
				require.Empty(t, auditStages)
				require.Zero(t, searchCalls)
				require.Equal(t, 1, forwards)
			case scenario.blockStage == "playground_search_query":
				require.Equal(t, []string{"playground_search_query"}, auditStages)
				require.Zero(t, searchCalls)
				require.Zero(t, *first)
			case scenario.blockStage == "playground_search_results":
				require.Equal(t, 1, searchCalls)
				require.Equal(t, 1, forwards)
				require.Equal(t, 1, *first)
			default:
				require.Equal(t, []string{"playground_search_query", "playground_search_results"}, auditStages)
				require.Equal(t, 1, searchCalls)
				require.Equal(t, 1, reserved)
				require.Equal(t, 2, forwards)
				require.Equal(t, 1, *first)
			}
		})
	}
}
