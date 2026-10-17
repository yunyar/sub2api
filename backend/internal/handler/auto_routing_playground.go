package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PlaygroundAutoRoutingHandler struct {
	routing *service.AutoRoutingService
}

func NewPlaygroundAutoRoutingHandler(routing *service.AutoRoutingService) *PlaygroundAutoRoutingHandler {
	return &PlaygroundAutoRoutingHandler{routing: routing}
}

type playgroundAutoRoutingResponse struct {
	ProfileID         int64   `json:"profile_id"`
	RoutingMode       string  `json:"routing_mode"`
	AutoProfileID     int64   `json:"auto_profile_id"`
	AutoSelectionMode string  `json:"auto_selection_mode"`
	GroupIDs          []int64 `json:"group_ids"`
	CrossGroupRetry   bool    `json:"cross_group_retry"`
	RoutingRevision   int64   `json:"routing_revision"`
}

func playgroundAutoRoutingResponseFrom(config *service.APIKeyAutoRoutingConfig) playgroundAutoRoutingResponse {
	groupIDs := append([]int64(nil), config.GroupIDs...)
	if groupIDs == nil {
		groupIDs = []int64{}
	}
	return playgroundAutoRoutingResponse{
		ProfileID:         config.ProfileID,
		RoutingMode:       service.AutoRoutingModeAuto,
		AutoProfileID:     config.ProfileID,
		AutoSelectionMode: config.SelectionMode,
		GroupIDs:          groupIDs,
		CrossGroupRetry:   config.CrossGroupRetry,
		RoutingRevision:   config.Revision,
	}
}

func playgroundAutoRoutingProfileID(c *gin.Context) (int64, bool) {
	profileID, err := strconv.ParseInt(c.Param("profile_id"), 10, 64)
	if err != nil || profileID <= 0 {
		response.BadRequest(c, "Invalid Auto profile ID")
		return 0, false
	}
	return profileID, true
}

func (h *PlaygroundAutoRoutingHandler) Get(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Authentication required")
		return
	}
	profileID, ok := playgroundAutoRoutingProfileID(c)
	if !ok {
		return
	}
	config, err := h.routing.GetPlaygroundAutoRouting(c.Request.Context(), subject.UserID, profileID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, playgroundAutoRoutingResponseFrom(config))
}

type playgroundAutoRoutingUpdateRequest struct {
	AutoSelectionMode string  `json:"auto_selection_mode"`
	GroupIDs          []int64 `json:"group_ids"`
	RoutingRevision   *int64  `json:"routing_revision"`
	CrossGroupRetry   *bool   `json:"cross_group_retry"`
}

func (h *PlaygroundAutoRoutingHandler) Update(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Authentication required")
		return
	}
	profileID, ok := playgroundAutoRoutingProfileID(c)
	if !ok {
		return
	}
	var request playgroundAutoRoutingUpdateRequest
	if c.ShouldBindJSON(&request) != nil || request.RoutingRevision == nil {
		response.BadRequest(c, "Valid Auto routing configuration and routing_revision are required")
		return
	}
	config := &service.APIKeyAutoRoutingConfig{
		Mode:          service.AutoRoutingModeAuto,
		ProfileID:     profileID,
		SelectionMode: request.AutoSelectionMode,
		GroupIDs:      append([]int64(nil), request.GroupIDs...),
	}
	if request.CrossGroupRetry != nil {
		config.CrossGroupRetry = *request.CrossGroupRetry
	}
	updated, err := h.routing.UpdatePlaygroundAutoRouting(
		c.Request.Context(), subject.UserID, profileID, config, *request.RoutingRevision,
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, playgroundAutoRoutingResponseFrom(updated))
}
