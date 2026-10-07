package provider

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestEasyPayRSA2NotificationVerifiesIdentityAndSignature(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	provider := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
	fields := easyPayRSANotifyFields(time.Now().Unix())
	easyPaySignRSAFields(t, fields, privateKey, "notify")

	notification, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(fields), nil)
	if err != nil {
		t.Fatalf("valid RSA2 notification rejected: %v", err)
	}
	if notification.Status != payment.ProviderStatusSuccess ||
		notification.OrderID != "order-42" ||
		notification.TradeNo != "gateway-42" ||
		notification.Amount != 12.34 ||
		notification.Metadata["pid"] != "pid-1" {
		t.Fatalf("unexpected notification: %+v", notification)
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{
			name: "wrong merchant",
			mutate: func(fields map[string]string) {
				fields["pid"] = "other-merchant"
			},
		},
		{
			name: "missing transaction id",
			mutate: func(fields map[string]string) {
				fields["trade_no"] = ""
			},
		},
		{
			name: "missing order id",
			mutate: func(fields map[string]string) {
				fields["out_trade_no"] = ""
			},
		},
		{
			name: "invalid amount",
			mutate: func(fields map[string]string) {
				fields["money"] = "NaN"
			},
		},
		{
			name: "wrong key id",
			mutate: func(fields map[string]string) {
				fields["key_id"] = "other-key"
			},
		},
		{
			name: "expired timestamp",
			mutate: func(fields map[string]string) {
				fields["timestamp"] = strconv.FormatInt(time.Now().Add(-6*time.Minute).Unix(), 10)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := cloneStringMap(fields)
			delete(mutated, "sign")
			test.mutate(mutated)
			easyPaySignRSAFields(t, mutated, privateKey, "notify")
			if _, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(mutated), nil); err == nil {
				t.Fatal("invalid RSA2 notification was accepted")
			}
		})
	}

	duplicate := encodeEasyPayRSAForm(fields) + "&money=12.34"
	if _, err := provider.VerifyNotification(context.Background(), duplicate, nil); err == nil {
		t.Fatal("duplicate RSA2 callback field was accepted")
	}
}

func TestEasyPayRSA2DoesNotDowngradeToMD5(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	provider := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
	fields := easyPayRSANotifyFields(time.Now().Unix())
	easyPaySignRSAFields(t, fields, privateKey, "notify")
	fields["sign_type"] = signTypeMD5
	if _, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(fields), nil); err == nil {
		t.Fatal("RSA2-configured provider accepted an MD5 downgrade")
	}
}

func TestEasyPayRSA2QueryOrderUsesNonceAndVerifiesSignedIdentity(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	queryNonce := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse query form: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		queryNonce = request.PostForm.Get("query_nonce")
		if request.PostForm.Get("gateway_sign_type") != signTypeRSA2 ||
			request.PostForm.Get("gateway_key_id") != "test-key-1" ||
			request.PostForm.Get("out_trade_no") != "order-42" {
			t.Errorf("unexpected RSA2 query request: %v", request.PostForm)
		}

		fields := map[string]string{
			"code": "1", "msg": "ok", "pid": "pid-1", "out_trade_no": "order-42",
			"trade_no": "gateway-42", "money": "12.34", "status": "1",
			"trade_status": tradeStatusSuccess, "query_nonce": queryNonce,
			"key_id": "test-key-1", "timestamp": strconv.FormatInt(time.Now().Unix(), 10),
			"nonce_str": strings.Repeat("a", 32), "sign_type": signTypeRSA2,
		}
		easyPaySignRSAFields(t, fields, privateKey, "query")
		response := make(map[string]any, len(fields))
		for key, value := range fields {
			switch key {
			case "code", "status":
				number, _ := strconv.Atoi(value)
				response[key] = number
			default:
				response[key] = value
			}
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode query response: %v", err)
		}
	}))
	defer server.Close()

	provider := newEasyPayRSAProvider(t, server.URL, &privateKey.PublicKey)
	result, err := provider.QueryOrder(context.Background(), "order-42")
	if err != nil {
		t.Fatalf("valid signed query response rejected: %v", err)
	}
	if queryNonce == "" || len(queryNonce) != 32 {
		t.Fatalf("query nonce = %q, want 32 hexadecimal characters", queryNonce)
	}
	if result.Status != payment.ProviderStatusPaid ||
		result.TradeNo != "gateway-42" ||
		result.Amount != 12.34 ||
		!result.SignatureVerified ||
		result.Metadata["pid"] != "pid-1" ||
		result.Metadata["out_trade_no"] != "order-42" ||
		result.Metadata["gateway_sign_type"] != signTypeRSA2 ||
		result.Metadata["gateway_key_id"] != "test-key-1" ||
		result.Metadata["gateway_public_key_sha256"] == "" {
		t.Fatalf("unexpected signed query response: %+v", result)
	}
}

func TestEasyPayRSA2QueryRejectsUnsignedMD5ShapedSuccess(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"code":1,"trade_status":"TRADE_SUCCESS","status":1,"money":"12.34","trade_no":"gateway-42","out_trade_no":"order-42"}`))
	}))
	defer server.Close()

	provider := newEasyPayRSAProvider(t, server.URL, &privateKey.PublicKey)
	if _, err := provider.QueryOrder(context.Background(), "order-42"); err == nil {
		t.Fatal("RSA2 query accepted an unsigned MD5-shaped success response")
	}
}

func TestEasyPayMD5NotificationCompatibilityAndStrictness(t *testing.T) {
	provider := newTestEasyPay(t, "http://gateway.invalid")
	fields := map[string]string{
		"pid": "pid-1", "out_trade_no": "order-legacy", "trade_no": "gateway-legacy",
		"type": "wxpay", "money": "12.34", "trade_status": tradeStatusSuccess,
	}
	fields["sign"] = easyPaySign(fields, "pkey-1")
	fields["sign_type"] = signTypeMD5
	notification, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(fields), nil)
	if err != nil {
		t.Fatalf("valid legacy MD5 notification rejected: %v", err)
	}
	if notification.OrderID != "order-legacy" || notification.TradeNo != "gateway-legacy" || notification.Amount != 12.34 {
		t.Fatalf("unexpected MD5 notification: %+v", notification)
	}
	lowercaseSignType := cloneStringMap(fields)
	lowercaseSignType["sign_type"] = "md5"
	if _, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(lowercaseSignType), nil); err != nil {
		t.Fatalf("legacy lowercase MD5 marker rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{
			name: "wrong merchant",
			mutate: func(fields map[string]string) {
				fields["pid"] = "other-merchant"
			},
		},
		{
			name: "empty transaction id",
			mutate: func(fields map[string]string) {
				fields["trade_no"] = ""
			},
		},
		{
			name: "empty order id",
			mutate: func(fields map[string]string) {
				fields["out_trade_no"] = ""
			},
		},
		{
			name: "non-finite amount",
			mutate: func(fields map[string]string) {
				fields["money"] = "Inf"
			},
		},
		{
			name: "RSA2 marker cannot be MD5-signed",
			mutate: func(fields map[string]string) {
				fields["sign_type"] = signTypeRSA2
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mutated := cloneStringMap(fields)
			delete(mutated, "sign")
			test.mutate(mutated)
			mutated["sign"] = easyPaySign(mutated, "pkey-1")
			if _, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(mutated), nil); err == nil {
				t.Fatal("invalid MD5 notification was accepted")
			}
		})
	}

	duplicate := encodeEasyPayRSAForm(fields) + "&trade_no=gateway-attacker"
	if _, err := provider.VerifyNotification(context.Background(), duplicate, nil); err == nil {
		t.Fatal("duplicate MD5 callback field was accepted")
	}
	if _, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(fields)+"&bad=%ZZ", nil); err == nil {
		t.Fatal("malformed MD5 callback query was accepted")
	}
}

func TestEasyPayCreateRequestMD5SignatureCannotBeReplayedAsPaymentCallback(t *testing.T) {
	var createRequest url.Values
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse create request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		createRequest = request.PostForm
		_, _ = writer.Write([]byte(`{"code":1,"msg":"ok"}`))
	}))
	defer server.Close()

	provider := newTestEasyPay(t, server.URL)
	_, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		PaymentType: "wxpay",
		OrderID:     "order-replay",
		Subject:     "Balance recharge",
		Amount:      "25.00",
		ClientIP:    "192.0.2.10",
	})
	if err != nil {
		t.Fatalf("create test order: %v", err)
	}
	if createRequest.Get("sign") == "" || createRequest.Get("sign_type") != signTypeMD5 {
		t.Fatalf("create request did not contain the expected MD5 signature: %v", createRequest)
	}

	if _, err := provider.VerifyNotification(context.Background(), createRequest.Encode(), nil); err == nil {
		t.Fatal("signed payment-creation request was accepted as a payment callback")
	}
	forgedCallback := make(url.Values, len(createRequest)+2)
	for key, values := range createRequest {
		for _, value := range values {
			forgedCallback.Add(key, value)
		}
	}
	forgedCallback.Set("trade_no", "attacker-injected-trade")
	forgedCallback.Set("trade_status", tradeStatusSuccess)
	if _, err := provider.VerifyNotification(context.Background(), forgedCallback.Encode(), nil); err == nil {
		t.Fatal("creation signature plus attacker-added payment fields was accepted as a callback")
	}
}

func TestEasyPayLegacyQueryRejectsConflictingIdentityAndDuplicateJSONKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "duplicate root key",
			body: `{"code":1,"code":1,"status":1,"money":"12.34","trade_no":"gateway-42"}`,
		},
		{
			name: "duplicate nested key",
			body: `{"code":1,"data":{"status":1,"status":1,"money":"12.34","trade_no":"gateway-42"}}`,
		},
		{
			name: "conflicting root and nested transaction ids",
			body: `{"code":1,"status":1,"money":"12.34","trade_no":"gateway-root","data":{"trade_no":"gateway-nested"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			if _, err := newTestEasyPay(t, server.URL).QueryOrder(context.Background(), "order-42"); err == nil {
				t.Fatal("ambiguous query response was accepted")
			}
		})
	}
}

func generateEasyPayRSATestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA test key: %v", err)
	}
	return privateKey
}

func newEasyPayRSAProvider(t *testing.T, apiBase string, publicKey *rsa.PublicKey) *EasyPay {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("marshal RSA public key: %v", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	provider, err := NewEasyPay("rsa-test", map[string]string{
		"pid": "pid-1", "pkey": "pkey-1", "apiBase": apiBase,
		"notifyUrl":       "https://merchant.example.test/notify",
		"returnUrl":       "https://merchant.example.test/return",
		"gatewaySignType": signTypeRSA2, "gatewayPublicKey": string(publicPEM),
		"gatewayKeyId": "test-key-1",
	})
	if err != nil {
		t.Fatalf("create RSA2 provider: %v", err)
	}
	return provider
}

func easyPayRSANotifyFields(timestamp int64) map[string]string {
	return map[string]string{
		"pid": "pid-1", "out_trade_no": "order-42", "trade_no": "gateway-42",
		"type": "wxpay", "name": "Recharge 中文", "money": "12.34",
		"trade_status": tradeStatusSuccess, "key_id": "test-key-1",
		"timestamp": strconv.FormatInt(timestamp, 10),
		"nonce_str": strings.Repeat("b", 32), "sign_type": signTypeRSA2,
	}
}

func easyPaySignRSAFields(t *testing.T, fields map[string]string, privateKey *rsa.PrivateKey, purpose string) {
	t.Helper()
	canonical, err := easyPayRSACanonical(fields, purpose)
	if err != nil {
		t.Fatalf("canonicalize RSA fields: %v", err)
	}
	digest := sha256.Sum256(canonical)
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign RSA fields: %v", err)
	}
	fields["sign"] = base64.StdEncoding.EncodeToString(signature)
	fields["sign_type"] = signTypeRSA2
}

func encodeEasyPayRSAForm(fields map[string]string) string {
	values := url.Values{}
	for key, value := range fields {
		values.Set(key, value)
	}
	return values.Encode()
}

func TestEasyPayRSAKeyConfigurationValidation(t *testing.T) {
	valid := map[string]string{
		"pid": "pid-1", "pkey": "pkey-1", "apiBase": "https://gateway.example.test",
		"notifyUrl": "https://merchant.example.test/notify",
		"returnUrl": "https://merchant.example.test/return",
	}
	for _, signType := range []string{"", "unknown"} {
		config := cloneStringMap(valid)
		config["gatewaySignType"] = signType
		if signType == "" {
			provider, err := NewEasyPay("legacy", config)
			if err != nil || provider.gatewaySignType != signTypeMD5 {
				t.Fatalf("empty sign type did not preserve MD5 default: provider=%+v err=%v", provider, err)
			}
			continue
		}
		if _, err := NewEasyPay("invalid", config); err == nil {
			t.Fatalf("unsupported signing type %q was accepted", signType)
		}
	}
	rsaConfig := cloneStringMap(valid)
	rsaConfig["gatewaySignType"] = signTypeRSA2
	rsaConfig["gatewayKeyId"] = "test-key-1"
	if _, err := NewEasyPay("missing-key", rsaConfig); err == nil {
		t.Fatal("RSA2 configuration without a public key was accepted")
	}
}

func TestEasyPayAmountParserRejectsNonFiniteAndPaddedValues(t *testing.T) {
	for _, value := range []string{"", " 1.00", "1.00 ", "NaN", "Inf", "-Inf", "not-an-amount"} {
		if _, err := parseEasyPayAmount(value); err == nil {
			t.Errorf("invalid amount %q was accepted", value)
		}
	}
	amount, err := parseEasyPayAmount(fmt.Sprint(12.34))
	if err != nil || amount != 12.34 {
		t.Fatalf("valid amount result = %v, %v", amount, err)
	}
}
