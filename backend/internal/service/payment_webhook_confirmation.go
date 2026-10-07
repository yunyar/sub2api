package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/shopspring/decimal"
)

var ErrPaymentGatewayConfirmation = errors.New("payment gateway confirmation failed")

func (s *PaymentService) confirmEasyPayCallback(
	ctx context.Context,
	order *dbent.PaymentOrder,
	notification *payment.PaymentNotification,
	result *payment.QueryOrderResponse,
) (*payment.QueryOrderResponse, error) {
	reject := func(reason string) (*payment.QueryOrderResponse, error) {
		if order != nil {
			s.writeAuditLog(ctx, order.ID, "PAYMENT_GATEWAY_CONFIRMATION_FAILED", payment.TypeEasyPay, map[string]any{
				"reason": reason,
			})
		}
		return nil, fmt.Errorf("%w: %s", ErrPaymentGatewayConfirmation, reason)
	}

	if order == nil || notification == nil {
		return reject("confirmation_input_missing")
	}
	if strings.TrimSpace(notification.TradeNo) == "" {
		return reject("callback_transaction_reference_missing")
	}
	if order.OutTradeNo == "" || notification.OrderID != order.OutTradeNo {
		return reject("order_reference_mismatch")
	}

	provider, err := s.getOrderProvider(ctx, order)
	if err != nil || provider == nil {
		return reject("provider_unavailable")
	}
	if !strings.EqualFold(strings.TrimSpace(provider.ProviderKey()), payment.TypeEasyPay) {
		return reject("provider_mismatch")
	}
	if err := s.validateEasyPayProviderSnapshot(ctx, order); err != nil {
		return reject("provider_configuration_mismatch")
	}

	if result == nil {
		finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
		result, err = provider.QueryOrder(ctx, order.OutTradeNo)
		finishProviderCall()
		if err != nil {
			return reject("gateway_query_failed")
		}
	}
	if result == nil {
		return reject("gateway_query_empty")
	}
	if result.Status != payment.ProviderStatusPaid {
		return reject("gateway_order_not_paid")
	}
	if responseOrderID := strings.TrimSpace(result.Metadata["out_trade_no"]); responseOrderID == "" || responseOrderID != order.OutTradeNo {
		return reject("gateway_order_reference_mismatch")
	}

	expectedIdentity := providerMerchantIdentityMetadata(provider)
	expectedMerchantID := strings.TrimSpace(expectedIdentity["pid"])
	queriedMerchantID := strings.TrimSpace(result.Metadata["pid"])
	callbackMerchantID := strings.TrimSpace(notification.Metadata["pid"])
	gatewayIdentity := strings.TrimSpace(expectedIdentity["gateway_identity"])
	snapshot := psOrderProviderSnapshot(order)
	if expectedMerchantID == "" ||
		gatewayIdentity == "" ||
		(snapshot != nil && snapshot.MerchantID != "" && snapshot.MerchantID != expectedMerchantID) ||
		(snapshot != nil && snapshot.GatewayIdentity != "" && snapshot.GatewayIdentity != gatewayIdentity) ||
		(order.ProviderGatewayIdentity != nil && strings.TrimSpace(*order.ProviderGatewayIdentity) != "" && strings.TrimSpace(*order.ProviderGatewayIdentity) != gatewayIdentity) ||
		queriedMerchantID != expectedMerchantID ||
		(callbackMerchantID != "" && callbackMerchantID != expectedMerchantID) {
		return reject("merchant_identity_mismatch")
	}
	if err := validateEasyPayQueryResponse(order, result); err != nil {
		return reject("provider_snapshot_mismatch")
	}

	orderCents, orderAmountOK := paymentAmountToCents(order.PayAmount)
	queriedCents, queriedAmountOK := paymentAmountToCents(result.Amount)
	if !orderAmountOK || !queriedAmountOK || orderCents != queriedCents {
		if orderAmountOK && queriedAmountOK && orderCents != queriedCents && isVerifiedEasyPayRSA2Query(result) {
			s.blockEasyPayRSA2AmountMismatch(ctx, order, expectedMerchantID, result)
		}
		return reject("payment_amount_mismatch")
	}
	callbackCents, callbackAmountOK := paymentAmountToCents(notification.Amount)
	if !callbackAmountOK || orderCents != callbackCents {
		return reject("callback_amount_mismatch")
	}
	if gatewayIdentity == "" {
		return reject("payment_amount_mismatch")
	}

	tradeNo := strings.TrimSpace(result.TradeNo)
	if tradeNo == "" || tradeNo != strings.TrimSpace(notification.TradeNo) {
		return reject("transaction_reference_mismatch")
	}
	if existingTradeNo := strings.TrimSpace(order.PaymentTradeNo); existingTradeNo != "" &&
		existingTradeNo != tradeNo && existingTradeNo != order.OutTradeNo {
		return reject("order_transaction_conflict")
	}

	reused, err := s.easyPayTransactionBoundToGateway(ctx, order, tradeNo, gatewayIdentity, expectedMerchantID)
	if err != nil {
		return reject("transaction_reuse_check_failed")
	}
	if reused {
		return reject("transaction_already_bound")
	}
	result.Metadata["sub2api_gateway_identity"] = gatewayIdentity

	s.writeAuditLog(ctx, order.ID, "PAYMENT_GATEWAY_CONFIRMED", payment.TypeEasyPay, map[string]any{
		"reason":   "authoritative_query_match",
		"provider": payment.TypeEasyPay,
	})
	return result, nil
}

func (s *PaymentService) confirmEasyPayFulfillment(ctx context.Context, order *dbent.PaymentOrder) error {
	if order == nil {
		return nil
	}
	providerKey := expectedNotificationProviderKeyForOrder(s.registry, order, "")
	if !strings.EqualFold(strings.TrimSpace(providerKey), payment.TypeEasyPay) {
		return nil
	}
	reject := func(reason string) error {
		s.writeAuditLog(ctx, order.ID, "PAYMENT_GATEWAY_CONFIRMATION_FAILED", payment.TypeEasyPay, map[string]any{
			"reason": reason,
			"source": "fulfillment_retry",
		})
		return fmt.Errorf("%w: %s", ErrPaymentGatewayConfirmation, reason)
	}
	if strings.TrimSpace(order.OutTradeNo) == "" {
		return reject("order_reference_missing")
	}

	provider, err := s.getOrderProvider(ctx, order)
	if err != nil || provider == nil {
		return reject("provider_unavailable")
	}
	if !strings.EqualFold(strings.TrimSpace(provider.ProviderKey()), payment.TypeEasyPay) {
		return reject("provider_mismatch")
	}
	if err := s.validateEasyPayProviderSnapshot(ctx, order); err != nil {
		return reject("provider_configuration_mismatch")
	}

	finishProviderCall := servertiming.ObserveDependency(ctx, "payment")
	result, err := provider.QueryOrder(ctx, order.OutTradeNo)
	finishProviderCall()
	if err != nil {
		return reject("gateway_query_failed")
	}
	if result == nil {
		return reject("gateway_query_empty")
	}
	if result.Status != payment.ProviderStatusPaid {
		return reject("gateway_order_not_paid")
	}
	if responseOrderID := strings.TrimSpace(result.Metadata["out_trade_no"]); responseOrderID == "" || responseOrderID != order.OutTradeNo {
		return reject("gateway_order_reference_mismatch")
	}
	if err := validateEasyPayQueryResponse(order, result); err != nil {
		return reject("provider_snapshot_mismatch")
	}

	expectedIdentity := providerMerchantIdentityMetadata(provider)
	expectedMerchantID := strings.TrimSpace(expectedIdentity["pid"])
	queriedMerchantID := strings.TrimSpace(result.Metadata["pid"])
	gatewayIdentity := strings.TrimSpace(expectedIdentity["gateway_identity"])
	snapshot := psOrderProviderSnapshot(order)
	if expectedMerchantID == "" ||
		gatewayIdentity == "" ||
		(snapshot != nil && snapshot.MerchantID != "" && snapshot.MerchantID != expectedMerchantID) ||
		(snapshot != nil && snapshot.GatewayIdentity != "" && snapshot.GatewayIdentity != gatewayIdentity) ||
		(order.ProviderGatewayIdentity != nil && strings.TrimSpace(*order.ProviderGatewayIdentity) != "" && strings.TrimSpace(*order.ProviderGatewayIdentity) != gatewayIdentity) ||
		queriedMerchantID != expectedMerchantID {
		return reject("merchant_identity_mismatch")
	}

	orderCents, orderAmountOK := paymentAmountToCents(order.PayAmount)
	queriedCents, queriedAmountOK := paymentAmountToCents(result.Amount)
	if !orderAmountOK || !queriedAmountOK || orderCents != queriedCents {
		if orderAmountOK && queriedAmountOK && orderCents != queriedCents && isVerifiedEasyPayRSA2Query(result) {
			s.blockEasyPayRSA2AmountMismatch(ctx, order, expectedMerchantID, result)
		}
		return reject("payment_amount_mismatch")
	}
	tradeNo := strings.TrimSpace(result.TradeNo)
	if tradeNo == "" {
		return reject("gateway_transaction_reference_missing")
	}
	if existingTradeNo := strings.TrimSpace(order.PaymentTradeNo); existingTradeNo != "" &&
		existingTradeNo != tradeNo && existingTradeNo != order.OutTradeNo {
		return reject("order_transaction_conflict")
	}

	reused, err := s.easyPayTransactionBoundToGateway(ctx, order, tradeNo, gatewayIdentity, expectedMerchantID)
	if err != nil {
		return reject("transaction_reuse_check_failed")
	}
	if reused {
		return reject("transaction_already_bound")
	}
	s.writeAuditLog(ctx, order.ID, "PAYMENT_GATEWAY_CONFIRMED", payment.TypeEasyPay, map[string]any{
		"reason":   "authoritative_query_match",
		"provider": payment.TypeEasyPay,
		"source":   "fulfillment_retry",
	})
	return nil
}

func (s *PaymentService) easyPayTransactionBoundToGateway(
	ctx context.Context,
	order *dbent.PaymentOrder,
	tradeNo string,
	gatewayIdentity string,
	merchantID string,
) (bool, error) {
	candidates, err := s.entClient.PaymentOrder.Query().
		Where(
			paymentorder.PaymentTradeNoEQ(tradeNo),
			paymentorder.IDNEQ(order.ID),
		).
		All(ctx)
	if err != nil {
		return false, err
	}

	for _, candidate := range candidates {
		if !strings.EqualFold(
			expectedNotificationProviderKeyForOrder(s.registry, candidate, ""),
			payment.TypeEasyPay,
		) {
			continue
		}
		candidateMerchantID, identityErr := s.easyPayOrderMerchantID(ctx, candidate)
		if identityErr != nil {
			return false, identityErr
		}
		candidateGatewayIdentity, identityErr := s.easyPayOrderGatewayIdentity(ctx, candidate)
		if identityErr != nil {
			return false, identityErr
		}
		if candidateGatewayIdentity != gatewayIdentity {
			continue
		}
		if candidateMerchantID != "" && merchantID != "" {
			if candidateMerchantID == merchantID {
				return true, nil
			}
			continue
		}
		return false, fmt.Errorf("matching transaction has unresolved merchant identity")
	}
	return false, nil
}

func (s *PaymentService) easyPayOrderMerchantID(ctx context.Context, order *dbent.PaymentOrder) (string, error) {
	if order != nil && order.ProviderMerchantID != nil && strings.TrimSpace(*order.ProviderMerchantID) != "" {
		return strings.TrimSpace(*order.ProviderMerchantID), nil
	}
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil && snapshot.MerchantID != "" {
		return snapshot.MerchantID, nil
	}
	provider, err := s.getOrderProvider(ctx, order)
	if err != nil {
		return "", err
	}
	if provider == nil || !strings.EqualFold(strings.TrimSpace(provider.ProviderKey()), payment.TypeEasyPay) {
		return "", fmt.Errorf("matching EasyPay transaction provider is unavailable")
	}
	return strings.TrimSpace(providerMerchantIdentityMetadata(provider)["pid"]), nil
}

func (s *PaymentService) easyPayOrderGatewayIdentity(ctx context.Context, order *dbent.PaymentOrder) (string, error) {
	if order != nil && order.ProviderGatewayIdentity != nil && strings.TrimSpace(*order.ProviderGatewayIdentity) != "" {
		return strings.TrimSpace(*order.ProviderGatewayIdentity), nil
	}
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil && snapshot.GatewayIdentity != "" {
		return snapshot.GatewayIdentity, nil
	}
	provider, err := s.getOrderProvider(ctx, order)
	if err != nil {
		return "", err
	}
	if provider == nil || !strings.EqualFold(strings.TrimSpace(provider.ProviderKey()), payment.TypeEasyPay) {
		return "", fmt.Errorf("matching EasyPay gateway is unavailable")
	}
	return strings.TrimSpace(providerMerchantIdentityMetadata(provider)["gateway_identity"]), nil
}

func validateEasyPayQueryResponse(order *dbent.PaymentOrder, result *payment.QueryOrderResponse) error {
	if result == nil {
		return fmt.Errorf("easypay query response is missing")
	}
	if err := validateEasyPayQueryMetadata(order, result.Metadata); err != nil {
		return err
	}
	responseSignType := normalizeEasyPayGatewaySignType(firstEasyPayMetadataValue(result.Metadata, "gateway_sign_type", "sign_type"))
	snapshotSignType := ""
	if snapshot := psOrderProviderSnapshot(order); snapshot != nil {
		snapshotSignType = normalizeEasyPayGatewaySignType(snapshot.GatewaySignType)
	}
	if responseSignType == "RSA2" || snapshotSignType == "RSA2" {
		if responseSignType != "RSA2" || !result.SignatureVerified {
			return fmt.Errorf("easypay RSA2 query was not cryptographically verified")
		}
	}
	return nil
}

func isVerifiedEasyPayRSA2Query(result *payment.QueryOrderResponse) bool {
	return result != nil &&
		result.SignatureVerified &&
		normalizeEasyPayGatewaySignType(firstEasyPayMetadataValue(result.Metadata, "gateway_sign_type", "sign_type")) == "RSA2"
}

func (s *PaymentService) blockEasyPayRSA2AmountMismatch(
	ctx context.Context,
	order *dbent.PaymentOrder,
	merchantID string,
	result *payment.QueryOrderResponse,
) {
	if order == nil || result == nil {
		return
	}
	auditErr := s.writeAuditLog(ctx, order.ID, "PAYMENT_RISK_CONFIRMED", payment.TypeEasyPay, map[string]any{
		"order_id":            order.ID,
		"expected_pay_amount": order.PayAmount,
		"queried_amount":      result.Amount,
		"query_out_trade_no":  strings.TrimSpace(result.Metadata["out_trade_no"]),
		"merchant":            strings.TrimSpace(merchantID),
		"trade_no":            strings.TrimSpace(result.TradeNo),
		"gateway_key_id":      firstEasyPayMetadataValue(result.Metadata, "gateway_key_id", "key_id"),
		"signatureVerified":   result.SignatureVerified,
	}
	if s.riskService == nil {
		return
	}
	if err := s.riskService.BlockOrderIP(ctx, order.ID, "easypay_rsa2_authoritative_amount_mismatch", "gateway-confirmation"); err != nil {
		_ = s.writeAuditLog(ctx, order.ID, "PAYMENT_GATEWAY_CONFIRMATION_FAILED", payment.TypeEasyPay, map[string]any{
			"reason": "automatic_ip_block_not_applied",
		})
	}
	if auditErr != nil {
		_ = s.writeAuditLog(ctx, order.ID, "PAYMENT_GATEWAY_CONFIRMATION_FAILED", payment.TypeEasyPay, map[string]any{
			"reason": "risk_audit_write_failed",
		})
	}
}

func validateEasyPayQueryMetadata(order *dbent.PaymentOrder, metadata map[string]string) error {
	if metadata == nil {
		metadata = map[string]string{}
	}
	if strings.TrimSpace(metadata["pid"]) != "" {
		if err := validateProviderNotificationMetadata(order, payment.TypeEasyPay, metadata); err != nil {
			return err
		}
	}
	snapshot := psOrderProviderSnapshot(order)
	signType := normalizeEasyPayGatewaySignType(firstEasyPayMetadataValue(metadata, "gateway_sign_type", "sign_type"))
	snapshotSignType := ""
	if snapshot != nil {
		snapshotSignType = normalizeEasyPayGatewaySignType(snapshot.GatewaySignType)
	}
	if snapshotSignType != "" && signType != "" && signType != snapshotSignType {
		return fmt.Errorf("easypay query sign type mismatch")
	}
	if snapshotSignType != "RSA2" && signType != "RSA2" {
		return nil
	}
	if signType != "RSA2" {
		return fmt.Errorf("easypay query RSA2 identity is missing")
	}
	if firstEasyPayMetadataValue(metadata, "pid") == "" ||
		firstEasyPayMetadataValue(metadata, "out_trade_no") == "" ||
		firstEasyPayMetadataValue(metadata, "gateway_key_id", "key_id") == "" ||
		firstEasyPayMetadataValue(metadata, "gateway_public_key_sha256", "public_key_sha256") == "" {
		return fmt.Errorf("easypay query RSA2 identity is incomplete")
	}
	if snapshot == nil {
		return nil
	}
	if keyID := firstEasyPayMetadataValue(metadata, "gateway_key_id", "key_id"); keyID != "" &&
		keyID != snapshot.GatewayKeyID {
		return fmt.Errorf("easypay query key id mismatch")
	}
	if fingerprint := firstEasyPayMetadataValue(metadata, "gateway_public_key_sha256", "public_key_sha256"); fingerprint != "" &&
		fingerprint != snapshot.GatewayPublicKeySHA256 {
		return fmt.Errorf("easypay query public key mismatch")
	}
	return nil
}

func firstEasyPayMetadataValue(metadata map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func (s *PaymentService) validateEasyPayProviderSnapshot(ctx context.Context, order *dbent.PaymentOrder) error {
	snapshot := psOrderProviderSnapshot(order)
	if snapshot == nil {
		return nil
	}
	if snapshot.ProviderKey != "" && !strings.EqualFold(strings.TrimSpace(snapshot.ProviderKey), payment.TypeEasyPay) {
		return fmt.Errorf("provider snapshot key mismatch")
	}
	instance, err := s.getOrderProviderInstance(ctx, order)
	if err != nil || instance == nil || s.loadBalancer == nil {
		return fmt.Errorf("provider snapshot instance unavailable")
	}
	config, err := s.loadBalancer.GetInstanceConfig(ctx, int64(instance.ID))
	if err != nil {
		return fmt.Errorf("provider snapshot config unavailable")
	}
	configSignType := normalizeEasyPayGatewaySignType(providerConfigFieldValue(config, "gatewaySignType"))
	snapshotSignType := normalizeEasyPayGatewaySignType(snapshot.GatewaySignType)
	if configSignType != snapshotSignType {
		return fmt.Errorf("provider snapshot sign type mismatch")
	}
	if configSignType != "RSA2" {
		return nil
	}
	if snapshot.GatewayKeyID == "" || snapshot.GatewayPublicKeySHA256 == "" ||
		snapshot.GatewayKeyID != strings.TrimSpace(providerConfigFieldValue(config, "gatewayKeyId")) ||
		snapshot.GatewayPublicKeySHA256 != easyPayPublicKeyFingerprint(providerConfigFieldValue(config, "gatewayPublicKey")) {
		return fmt.Errorf("provider snapshot RSA identity mismatch")
	}
	return nil
}

func paymentAmountToCents(amount float64) (int64, bool) {
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, false
	}
	scaled := decimal.NewFromFloat(amount).Mul(decimal.NewFromInt(100))
	if !scaled.Equal(scaled.Truncate(0)) {
		return 0, false
	}
	cents, err := strconv.ParseInt(scaled.StringFixed(0), 10, 64)
	if err != nil || cents <= 0 {
		return 0, false
	}
	return cents, true
}
