package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
)

type AuthIPRiskService interface {
	CheckIP(ctx context.Context, rawIP string) error
	RecordUserIP(ctx context.Context, userID int64, rawIP, source string) error
}

type paymentRiskIPContextKey struct{}

func WithPaymentRiskIP(ctx context.Context, rawIP string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, paymentRiskIPContextKey{}, rawIP)
}

func PaymentRiskIPFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	rawIP, ok := ctx.Value(paymentRiskIPContextKey{}).(string)
	return rawIP, ok
}

func (s *AuthService) SetPaymentRiskService(riskService AuthIPRiskService) {
	s.paymentRiskService = riskService
}

func trustedPublicSessionIP(ctx context.Context) string {
	rawIP, exists := PaymentRiskIPFromContext(ctx)
	if !exists {
		return ""
	}
	return ip.PublicClientIP(rawIP)
}

func (s *AuthService) checkRegistrationIPRisk(ctx context.Context) error {
	address := trustedPublicSessionIP(ctx)
	if address == "" {
		return nil
	}
	if s == nil || s.paymentRiskService == nil {
		return ErrServiceUnavailable
	}
	return s.paymentRiskService.CheckIP(ctx, address)
}

func (s *AuthService) recordUserIP(ctx context.Context, userID int64, source string) error {
	address := trustedPublicSessionIP(ctx)
	if address == "" {
		return nil
	}
	if s == nil || s.paymentRiskService == nil {
		return ErrServiceUnavailable
	}
	return s.paymentRiskService.RecordUserIP(ctx, userID, address, source)
}
