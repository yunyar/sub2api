package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterPlaygroundAutoRoutingRoutes(
	authenticated *gin.RouterGroup,
	autoRouting *handler.PlaygroundAutoRoutingHandler,
) {
	group := authenticated.Group("/playground/auto-routing")
	group.GET("/:profile_id", autoRouting.Get)
	group.PUT("/:profile_id", autoRouting.Update)
}
