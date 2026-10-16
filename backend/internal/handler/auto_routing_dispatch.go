package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func beginAutoRoutingDispatch(c *gin.Context, key *service.APIKey, apiKeys *service.APIKeyService, account *service.Account) bool {
	if key == nil || key.AutoRouting == nil {
		return true
	}
	routing := apiKeys.AutoRoutingService()
	if !middleware.DispatchAutoRouting(c, key, routing) {
		return false
	}
	if err := routing.RecordDispatchAccount(c.Request.Context(), key, account); err != nil {
		if c.Writer.Written() {
			c.Abort()
			return false
		}
		middleware.AbortWithError(c, 503, "AUTO_ROUTING_SNAPSHOT_UNAVAILABLE", "Unable to persist Auto routing execution snapshot")
		return false
	}
	if err := routing.AdmitDispatch(c.Request.Context(), key); err != nil {
		if c.Writer.Written() {
			c.Abort()
			return false
		}
		status := http.StatusServiceUnavailable
		code := "AUTO_ROUTING_ADMISSION_UNAVAILABLE"
		if errors.Is(err, service.ErrGroupRPMExceeded) || errors.Is(err, service.ErrUserRPMExceeded) {
			status = http.StatusTooManyRequests
			code = "rate_limit_exceeded"
		}
		middleware.AbortWithError(c, status, code, err.Error())
		return false
	}
	middleware.MarkAutoRoutingUpstreamStarted(c)
	return true
}

func completeAutoRoutingDispatch(c *gin.Context, reference string) {
	if reference == "" {
		reference = "response_completed"
	}
	middleware.SetAutoRoutingResultReference(c, reference)
}
