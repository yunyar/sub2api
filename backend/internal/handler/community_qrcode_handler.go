package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CommunityQRCodeHandler struct {
	settingService *service.SettingService
}

func NewCommunityQRCodeHandler(settingService *service.SettingService) *CommunityQRCodeHandler {
	return &CommunityQRCodeHandler{settingService: settingService}
}

func (h *CommunityQRCodeHandler) List(c *gin.Context) {
	items, err := h.settingService.GetCommunityQRCodes(c.Request.Context(), true)
	if err != nil {
		response.InternalError(c, "Failed to load community QR codes")
		return
	}
	response.Success(c, items)
}

func (h *CommunityQRCodeHandler) AdminList(c *gin.Context) {
	items, err := h.settingService.GetCommunityQRCodes(c.Request.Context(), false)
	if err != nil {
		response.InternalError(c, "Failed to load community QR codes")
		return
	}
	response.Success(c, items)
}

type updateCommunityQRCodesRequest struct {
	Items []service.CommunityQRCode `json:"items"`
}

func (h *CommunityQRCodeHandler) AdminUpdate(c *gin.Context) {
	var req updateCommunityQRCodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request body")
		return
	}
	items, err := h.settingService.SetCommunityQRCodes(c.Request.Context(), req.Items)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, response.Response{Code: 0, Message: "success", Data: items})
}
