package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func retryAutoRoutingAfterConfirmedFailure(
	c *gin.Context,
	apiKeys *service.APIKeyService,
	failoverErr *service.UpstreamFailoverError,
) (*service.APIKey, *service.UserSubscription, bool, error) {
	if c == nil || apiKeys == nil {
		return nil, nil, false, nil
	}
	if failoverErr == nil || !failoverErr.ShouldRetryNextAccount() ||
		(failoverErr.StatusCode < http.StatusBadRequest && !failoverErr.IsCredentialFailure()) ||
		(c.Writer != nil && c.Writer.Size() > 0) {
		return nil, nil, false, nil
	}
	next, switched, err := middleware.RetryAutoRoutingGroup(
		c,
		apiKeys.AutoRoutingService(),
		service.AutoRouteEvidenceUpstreamFailure,
	)
	if err != nil || !switched || next == nil {
		return nil, nil, false, err
	}
	subscription, _ := middleware.GetSubscriptionFromContext(c)
	return next, subscription, true, nil
}

func autoRoutingGroupBillingEligible(
	c *gin.Context,
	billing *service.BillingCacheService,
	key *service.APIKey,
	subscription *service.UserSubscription,
) bool {
	if c == nil || billing == nil || key == nil {
		return false
	}
	return billing.CheckBillingEligibility(
		c.Request.Context(),
		key.User,
		key,
		key.Group,
		subscription,
		service.QuotaPlatform(c.Request.Context(), key),
	) == nil
}

func beginAutoRoutingDispatch(c *gin.Context, key *service.APIKey, apiKeys *service.APIKeyService, account *service.Account) bool {
	if key == nil || key.AutoRouting == nil {
		return true
	}
	resuming := middleware.AutoRoutingDispatchStarted(c, key)
	routing := apiKeys.AutoRoutingService()
	if (!resuming || routing == nil) && !middleware.DispatchAutoRouting(c, key, routing) {
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
	if !resuming {
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
