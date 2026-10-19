package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *AutoRoutingHandler) PreviewModelCatalog(c *gin.Context) {
	var request struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Invalid Auto model catalog request")
		return
	}
	capabilities, err := h.routing.PreviewModelCatalog(c.Request.Context(), request.GroupIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	models := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		models = append(models, capability.ModelID)
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, gin.H{"models": models})
}
