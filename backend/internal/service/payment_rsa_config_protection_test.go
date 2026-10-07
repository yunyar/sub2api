//go:build unit

package service

import (
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestEasyPayRSAConfigChangesAreProtectedWhileOrdersArePending(t *testing.T) {
	current := map[string]string{
		"pid":              "merchant-1",
		"pkey":             "request-authentication-secret",
		"apiBase":          "https://gateway.example",
		"gatewaySignType":  "RSA2",
		"gatewayPublicKey": "public-key-pem",
		"gatewayKeyId":     "gateway-key-1",
	}
	for _, field := range []string{"pkey", "pid", "apiBase", "gatewaySignType", "gatewayPublicKey", "gatewayKeyId"} {
		t.Run(field, func(t *testing.T) {
			next := make(map[string]string, len(current))
			for key, value := range current {
				next[key] = value
			}
			next[field] = "changed"
			require.True(t, hasPendingOrderProtectedConfigChange(payment.TypeEasyPay, current, next))
		})
	}
	require.False(t, hasPendingOrderProtectedConfigChange(payment.TypeEasyPay, current, current))
}

func TestEasyPayDefaultAndExplicitMD5AreEquivalentForPendingOrderProtection(t *testing.T) {
	current := map[string]string{"pid": "merchant-1", "pkey": "request-authentication-secret"}
	for _, signType := range []string{"", "md5", " MD5 "} {
		next := map[string]string{
			"pid": "merchant-1", "pkey": "request-authentication-secret", "gatewaySignType": signType,
		}
		require.False(t, hasPendingOrderProtectedConfigChange(payment.TypeEasyPay, current, next))
	}
	require.Equal(t, "MD5", normalizeEasyPayGatewaySignType(""))
	require.Equal(t, "RSA2", normalizeEasyPayGatewaySignType(" rsa2 "))
}

func TestEasyPayRSASnapshotStoresIdentityWithoutPublicKeyMaterial(t *testing.T) {
	publicKey := "-----BEGIN PUBLIC KEY-----\npublic-key-bytes\n-----END PUBLIC KEY-----"
	snapshot := buildPaymentOrderProviderSnapshot(&payment.InstanceSelection{
		InstanceID:  "42",
		ProviderKey: payment.TypeEasyPay,
		PaymentMode: "popup",
		Config: map[string]string{
			"pid":              "merchant-1",
			"pkey":             "request-authentication-secret",
			"gatewaySignType":  "RSA2",
			"gatewayPublicKey": publicKey,
			"gatewayKeyId":     "gateway-key-1",
		},
	}, CreateOrderRequest{})

	require.Equal(t, 3, snapshot["schema_version"])
	require.Equal(t, "RSA2", snapshot["gateway_sign_type"])
	require.Equal(t, "gateway-key-1", snapshot["gateway_key_id"])
	require.Equal(t, easyPayPublicKeyFingerprint(publicKey), snapshot["gateway_public_key_sha256"])
	require.NotContains(t, snapshot, "gatewayPublicKey")
	require.NotContains(t, snapshot, "pkey")
	require.NotContains(t, snapshot, publicKey)

	parsed := psOrderProviderSnapshot(&dbent.PaymentOrder{ProviderSnapshot: snapshot})
	require.Equal(t, "RSA2", parsed.GatewaySignType)
	require.Equal(t, "gateway-key-1", parsed.GatewayKeyID)
	require.Equal(t, easyPayPublicKeyFingerprint(publicKey), parsed.GatewayPublicKeySHA256)
}

func TestEasyPayQueryMetadataChecksRSAIdentityWhenPresent(t *testing.T) {
	order := &dbent.PaymentOrder{
		ProviderSnapshot: map[string]any{
			"schema_version":            3,
			"provider_key":              payment.TypeEasyPay,
			"merchant_id":               "merchant-1",
			"gateway_sign_type":         "RSA2",
			"gateway_key_id":            "gateway-key-1",
			"gateway_public_key_sha256": "fingerprint-1",
		},
	}
	require.NoError(t, validateEasyPayQueryMetadata(order, map[string]string{
		"pid": "merchant-1", "out_trade_no": "order-1", "sign_type": "RSA2",
		"key_id": "gateway-key-1", "public_key_sha256": "fingerprint-1",
	}))
	require.Error(t, validateEasyPayQueryMetadata(order, map[string]string{
		"pid": "merchant-1", "out_trade_no": "order-1", "key_id": "gateway-key-1",
	}))
	require.Error(t, validateEasyPayQueryMetadata(order, map[string]string{"pid": "merchant-2"}))
	require.Error(t, validateEasyPayQueryMetadata(order, map[string]string{"key_id": "gateway-key-2"}))
	require.Error(t, validateEasyPayQueryMetadata(order, map[string]string{"sign_type": "MD5"}))
	require.Error(t, validateEasyPayQueryMetadata(order, nil))
}
