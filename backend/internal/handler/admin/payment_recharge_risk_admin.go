package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type blockPaymentRiskOrderRequest struct {
	OrderID int64  `json:"order_id" binding:"required,gt=0"`
	Reason  string `json:"reason" binding:"required"`
	Confirm bool   `json:"confirm"`
}

type unblockPaymentRiskIPRequest struct {
	IP      string `json:"ip" binding:"required"`
	Reason  string `json:"reason" binding:"required"`
	Confirm bool   `json:"confirm"`
}

type scanPaymentRiskAccountsRequest struct {
	Confirm bool `json:"confirm"`
}

func (h *PaymentHandler) paymentRiskService(c *gin.Context) *service.PaymentRiskService {
	if h == nil || h.paymentService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Payment risk service unavailable")
		return nil
	}
	riskService := h.paymentService.GetRiskService()
	if riskService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Payment risk service unavailable")
		return nil
	}
	return riskService
}

func paymentRiskActor(c *gin.Context) (string, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		return "", false
	}
	email := strings.TrimSpace(c.GetString(middleware.ContextKeyAuthEmail))
	if email != "" && len(email) <= 128 {
		return email, true
	}
	return strconv.FormatInt(subject.UserID, 10), true
}

// ListRiskIPs returns both active blocks and retained released records.
// GET /api/v1/admin/payment/risk-ips
func (h *PaymentHandler) ListRiskIPs(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	if pageSize > 100 {
		pageSize = 100
	}
	riskService := h.paymentRiskService(c)
	if riskService == nil {
		return
	}
	records, total, err := riskService.ListBlockedIPs(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, records, int64(total), page, pageSize)
}

// BlockRiskOrderIP permanently blocks the public client IP stored on an order.
// POST /api/v1/admin/payment/risk-ips/block-order
func (h *PaymentHandler) BlockRiskOrderIP(c *gin.Context) {
	var request blockPaymentRiskOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		response.BadRequest(c, "A valid order, reason, and explicit confirmation are required")
		return
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Reason == "" || len(request.Reason) > 512 {
		response.BadRequest(c, "Reason must contain 1 to 512 bytes")
		return
	}
	actor, ok := paymentRiskActor(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	riskService := h.paymentRiskService(c)
	if riskService == nil {
		return
	}
	if err := riskService.BlockOrderIP(c.Request.Context(), request.OrderID, request.Reason, actor); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"blocked": true})
}

// UnblockRiskIP releases an IP block without restoring linked account status.
// POST /api/v1/admin/payment/risk-ips/unblock
func (h *PaymentHandler) UnblockRiskIP(c *gin.Context) {
	var request unblockPaymentRiskIPRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		response.BadRequest(c, "A public IP, reason, and explicit confirmation are required")
		return
	}
	request.IP = strings.TrimSpace(request.IP)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.IP == "" || request.Reason == "" || len(request.Reason) > 512 {
		response.BadRequest(c, "A public IP and a reason of 1 to 512 bytes are required")
		return
	}
	actor, ok := paymentRiskActor(c)
	if !ok {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	riskService := h.paymentRiskService(c)
	if riskService == nil {
		return
	}
	if err := riskService.UnblockIP(c.Request.Context(), request.IP, actor, request.Reason); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"released": true, "accounts_restored": false})
}

// ScanRiskAccounts reapplies active IP policies to accounts linked since the last scan.
// POST /api/v1/admin/payment/risk-ips/scan
func (h *PaymentHandler) ScanRiskAccounts(c *gin.Context) {
	var request scanPaymentRiskAccountsRequest
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		response.BadRequest(c, "Explicit confirmation is required to scan linked accounts")
		return
	}
	riskService := h.paymentRiskService(c)
	if riskService == nil {
		return
	}
	restricted, err := riskService.ScanBlockedAccounts(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"restricted_accounts": restricted})
}
