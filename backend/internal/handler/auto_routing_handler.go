package handler

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AutoRoutingHandler struct {
	routing *service.AutoRoutingService
}

func NewAutoRoutingHandler(routing *service.AutoRoutingService) *AutoRoutingHandler {
	return &AutoRoutingHandler{routing: routing}
}

type autoRoutingProfileResponse struct {
	ID                     int64                                 `json:"id"`
	Name                   string                                `json:"name"`
	Status                 string                                `json:"status"`
	Revision               int64                                 `json:"revision"`
	MaxCandidates          int                                   `json:"max_candidates"`
	AllowCustomOrder       bool                                  `json:"allow_custom_order"`
	CrossGroupRetryAllowed bool                                  `json:"cross_group_retry_allowed"`
	ModelCatalogFilter     autoRoutingModelCatalogFilterResponse `json:"model_catalog_filter"`
	GroupIDs               []int64                               `json:"group_ids"`
	CreatedAt              time.Time                             `json:"created_at"`
	UpdatedAt              time.Time                             `json:"updated_at"`
}

type autoRoutingModelCatalogFilterResponse struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models"`
}

func autoProfileModelCatalogFilter(profile *service.AutoRoutingProfile) autoRoutingModelCatalogFilterResponse {
	filter := autoRoutingModelCatalogFilterResponse{Models: []string{}}
	if profile == nil || profile.ModelCatalogFilter == nil {
		return filter
	}
	filter.Enabled = profile.ModelCatalogFilter.Enabled
	filter.Models = append(filter.Models, profile.ModelCatalogFilter.Models...)
	return filter
}

func autoProfileResponse(profile *service.AutoRoutingProfile) autoRoutingProfileResponse {
	groupIDs := make([]int64, 0, len(profile.Members))
	for _, member := range profile.Members {
		groupIDs = append(groupIDs, member.GroupID)
	}
	return autoRoutingProfileResponse{
		ID: profile.ID, Name: profile.Name, Status: profile.Status, Revision: profile.Revision,
		MaxCandidates: profile.MaxCandidates, AllowCustomOrder: profile.AllowCustomOrder,
		CrossGroupRetryAllowed: profile.CrossGroupRetryAllowed,
		ModelCatalogFilter:     autoProfileModelCatalogFilter(profile),
		GroupIDs:               groupIDs,
		CreatedAt:              profile.CreatedAt, UpdatedAt: profile.UpdatedAt,
	}
}

func autoRoutingID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid Auto routing ID")
		return 0, false
	}
	return id, true
}

type autoProfileWriteRequest struct {
	Name                   string                       `json:"name"`
	Status                 string                       `json:"status"`
	Revision               int64                        `json:"revision"`
	MaxCandidates          int                          `json:"max_candidates"`
	AllowCustomOrder       bool                         `json:"allow_custom_order"`
	CrossGroupRetryAllowed bool                         `json:"cross_group_retry_allowed"`
	ModelCatalogFilter     *service.GroupModelAllowlist `json:"model_catalog_filter"`
	GroupIDs               []int64                      `json:"group_ids"`
}

func (request autoProfileWriteRequest) profile(id int64) *service.AutoRoutingProfile {
	members := make([]service.AutoRoutingProfileMember, len(request.GroupIDs))
	for index, groupID := range request.GroupIDs {
		members[index] = service.AutoRoutingProfileMember{GroupID: groupID, SortOrder: index}
	}
	return &service.AutoRoutingProfile{ID: id, Name: request.Name, Status: request.Status,
		Revision: request.Revision, MaxCandidates: request.MaxCandidates, AllowCustomOrder: request.AllowCustomOrder,
		CrossGroupRetryAllowed: request.CrossGroupRetryAllowed,
		ModelCatalogFilter:     request.ModelCatalogFilter, Members: members}
}

func (h *AutoRoutingHandler) ListProfiles(c *gin.Context) {
	profiles, err := h.routing.ListProfiles(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]autoRoutingProfileResponse, 0, len(profiles))
	for index := range profiles {
		items = append(items, autoProfileResponse(&profiles[index]))
	}
	response.Success(c, items)
}

func (h *AutoRoutingHandler) GetProfile(c *gin.Context) {
	id, ok := autoRoutingID(c, "id")
	if !ok {
		return
	}
	profile, err := h.routing.GetProfile(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, autoProfileResponse(profile))
}

func (h *AutoRoutingHandler) CreateProfile(c *gin.Context) {
	request := autoProfileWriteRequest{Status: "disabled", MaxCandidates: 10, AllowCustomOrder: true}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid Auto profile")
		return
	}
	profile, err := h.routing.CreateProfile(c.Request.Context(), request.profile(0))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, autoProfileResponse(profile))
}

func (h *AutoRoutingHandler) UpdateProfile(c *gin.Context) {
	id, ok := autoRoutingID(c, "id")
	if !ok {
		return
	}
	var request autoProfileWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid Auto profile")
		return
	}
	profile, err := h.routing.UpdateProfile(c.Request.Context(), request.profile(id), request.Revision)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, autoProfileResponse(profile))
}

func (h *AutoRoutingHandler) DisableProfile(c *gin.Context) {
	id, ok := autoRoutingID(c, "id")
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revision <= 0 {
		response.BadRequest(c, "Profile revision is required")
		return
	}
	profile, err := h.routing.DisableProfile(c.Request.Context(), id, revision)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, autoProfileResponse(profile))
}

func (h *AutoRoutingHandler) AvailableProfiles(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	profiles, err := h.routing.AvailableProfiles(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]autoRoutingProfileResponse, 0, len(profiles))
	for index := range profiles {
		items = append(items, autoProfileResponse(&profiles[index]))
	}
	response.Success(c, items)
}

type autoKeyRoutingResponse struct {
	RoutingMode       string  `json:"routing_mode"`
	AutoProfileID     *int64  `json:"auto_profile_id"`
	AutoSelectionMode string  `json:"auto_selection_mode"`
	GroupIDs          []int64 `json:"group_ids"`
	CrossGroupRetry   bool    `json:"cross_group_retry"`
	RoutingRevision   int64   `json:"routing_revision"`
	GroupID           *int64  `json:"group_id"`
}

func autoKeyResponse(state *service.APIKeyRoutingState) autoKeyRoutingResponse {
	result := autoKeyRoutingResponse{RoutingMode: state.Mode, RoutingRevision: state.Revision,
		GroupID: state.FixedGroupID, AutoSelectionMode: "inherit", GroupIDs: []int64{}}
	if state.Auto != nil {
		profileID := state.Auto.ProfileID
		result.AutoProfileID = &profileID
		result.AutoSelectionMode = state.Auto.SelectionMode
		result.GroupIDs = append(result.GroupIDs, state.Auto.GroupIDs...)
		result.CrossGroupRetry = state.Auto.CrossGroupRetry
	}
	return result
}

func (h *AutoRoutingHandler) GetKeyRouting(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	id, ok := autoRoutingID(c, "id")
	if !ok {
		return
	}
	state, err := h.routing.GetKeyRoutingState(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, autoKeyResponse(state))
}

type autoKeyRoutingWriteRequest struct {
	RoutingMode       *string  `json:"routing_mode"`
	AutoProfileID     *int64   `json:"auto_profile_id"`
	AutoSelectionMode *string  `json:"auto_selection_mode"`
	GroupIDs          *[]int64 `json:"group_ids"`
	CrossGroupRetry   *bool    `json:"cross_group_retry"`
	RoutingRevision   *int64   `json:"routing_revision"`
	GroupID           *int64   `json:"group_id"`
}

func (h *AutoRoutingHandler) UpdateKeyRouting(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	id, ok := autoRoutingID(c, "id")
	if !ok {
		return
	}
	var request autoKeyRoutingWriteRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.RoutingRevision == nil {
		response.BadRequest(c, "Valid routing configuration and routing_revision are required")
		return
	}
	state, err := h.routing.GetKeyRoutingState(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	mode := state.Mode
	if request.RoutingMode != nil {
		mode = *request.RoutingMode
	}
	var config *service.APIKeyAutoRoutingConfig
	fixedGroupID := request.GroupID
	switch mode {
	case "auto":
		config = &service.APIKeyAutoRoutingConfig{Mode: "auto", SelectionMode: "inherit"}
		if state.Auto != nil {
			*config = *state.Auto
		}
		if request.AutoProfileID != nil {
			config.ProfileID = *request.AutoProfileID
		}
		if request.AutoSelectionMode != nil {
			config.SelectionMode = *request.AutoSelectionMode
		}
		if request.GroupIDs != nil {
			config.GroupIDs = append([]int64(nil), (*request.GroupIDs)...)
		}
		if request.CrossGroupRetry != nil {
			config.CrossGroupRetry = *request.CrossGroupRetry
		}
		if config.SelectionMode == "inherit" {
			config.GroupIDs = nil
		}
	case "fixed":
		if request.AutoProfileID != nil || (request.GroupIDs != nil && len(*request.GroupIDs) > 0) || (request.CrossGroupRetry != nil && *request.CrossGroupRetry) {
			response.BadRequest(c, "Fixed routing cannot contain Auto candidates")
			return
		}
		if fixedGroupID == nil {
			fixedGroupID = state.FixedGroupID
		}
	default:
		response.BadRequest(c, "Invalid routing mode")
		return
	}
	_, err = h.routing.UpdateKeyRouting(c.Request.Context(), subject.UserID, id, config, fixedGroupID, *request.RoutingRevision)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	state, err = h.routing.GetKeyRoutingState(c.Request.Context(), subject.UserID, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, autoKeyResponse(state))
}
