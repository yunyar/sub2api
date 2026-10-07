package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

type easyPayRSAContractVector struct {
	PublicKey string            `json:"public_key"`
	Signature string            `json:"signature"`
	Fields    map[string]string `json:"fields"`
}

func TestEasyPayRSACrossLanguageVector(t *testing.T) {
	vector := readEasyPayRSAContractVector(t)
	publicKey, err := parseEasyPayRSAPublicKey(vector.PublicKey)
	if err != nil {
		t.Fatalf("parse RSA contract-vector public key: %v", err)
	}
	fields := cloneStringMap(vector.Fields)
	fields["sign"] = vector.Signature
	fields["sign_type"] = signTypeRSA2
	unixTime, err := strconv.ParseInt(fields["timestamp"], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(unixTime, 0)
	if err := verifyEasyPayRSA(fields, publicKey, "test_key-1", "notify", now); err != nil {
		t.Fatalf("verify RSA contract-vector signature: %v", err)
	}
	canonical, err := easyPayRSACanonical(vector.Fields, "notify")
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := "sub2api-easypay-notify-v1\nkey_id=test_key-1&money=12.34&name=%E4%B8%AD%E6%96%87%20%E7%A9%BA%E6%A0%BC%20%26%2B%3D~&nonce_str=00112233445566778899aabbccddeeff&out_trade_no=%E8%AE%A2%E5%8D%95%201&pid=1001&timestamp=1791111111&trade_no=gateway-1&trade_status=TRADE_SUCCESS&type=wxpay"
	if string(canonical) != wantCanonical {
		t.Fatalf("canonical = %q, want %q", canonical, wantCanonical)
	}

	tests := []struct {
		name    string
		mutate  func(map[string]string)
		context string
		keyID   string
		now     time.Time
	}{
		{name: "wrong purpose", context: "query", keyID: "test_key-1", now: now},
		{name: "wrong key id", context: "notify", keyID: "another-key", now: now},
		{name: "expired timestamp", context: "notify", keyID: "test_key-1", now: now.Add(301 * time.Second)},
		{name: "MD5 downgrade", context: "notify", keyID: "test_key-1", now: now, mutate: func(fields map[string]string) { fields["sign_type"] = "MD5" }},
		{name: "tampered amount", context: "notify", keyID: "test_key-1", now: now, mutate: func(fields map[string]string) { fields["money"] = "99.99" }},
		{name: "malformed nonce", context: "notify", keyID: "test_key-1", now: now, mutate: func(fields map[string]string) { fields["nonce_str"] = "001122" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := cloneStringMap(fields)
			if test.mutate != nil {
				test.mutate(params)
			}
			if err := verifyEasyPayRSA(params, publicKey, test.keyID, test.context, test.now); err == nil {
				t.Fatal("verification unexpectedly succeeded")
			}
		})
	}
	otherKey := generateEasyPayRSATestKey(t)
	if err := verifyEasyPayRSA(fields, &otherKey.PublicKey, "test_key-1", "notify", now); err == nil {
		t.Fatal("verification unexpectedly accepted the wrong public key")
	}
}

func TestEasyPayRSANotifyGatewayContract(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	provider := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
	params := easyPayRSANotifyFields(time.Now().Unix())
	easyPaySignRSAFields(t, params, privateKey, "notify")
	body := encodeEasyPayRSAForm(params)
	notification, err := provider.VerifyNotification(context.Background(), body, nil)
	if err != nil {
		t.Fatalf("VerifyNotification rejected valid RSA2 notification: %v", err)
	}
	if notification.Status != payment.ProviderStatusSuccess || notification.OrderID != "order-42" ||
		notification.TradeNo != "gateway-42" || notification.Amount != 12.34 {
		t.Fatalf("unexpected notification: %+v", notification)
	}

	tests := []struct {
		name string
		body func() string
	}{
		{name: "tampered order", body: func() string {
			changed := cloneStringMap(params)
			changed["out_trade_no"] = "other-order"
			return encodeEasyPayRSAForm(changed)
		}},
		{name: "wrong key id", body: func() string {
			changed := cloneStringMap(params)
			delete(changed, "sign")
			changed["key_id"] = "other-key"
			easyPaySignRSAFields(t, changed, privateKey, "notify")
			return encodeEasyPayRSAForm(changed)
		}},
		{name: "query-purpose signature", body: func() string {
			changed := cloneStringMap(params)
			delete(changed, "sign")
			easyPaySignRSAFields(t, changed, privateKey, "query")
			return encodeEasyPayRSAForm(changed)
		}},
		{name: "expired signature", body: func() string {
			changed := easyPayRSANotifyFields(time.Now().Add(-301 * time.Second).Unix())
			easyPaySignRSAFields(t, changed, privateKey, "notify")
			return encodeEasyPayRSAForm(changed)
		}},
		{name: "MD5 downgrade", body: func() string {
			changed := cloneStringMap(params)
			changed["sign_type"] = "MD5"
			return encodeEasyPayRSAForm(changed)
		}},
		{name: "duplicate field", body: func() string {
			return encodeEasyPayRSAForm(params) + "&money=12.34"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := provider.VerifyNotification(context.Background(), test.body(), nil); err == nil {
				t.Fatal("VerifyNotification unexpectedly accepted invalid notification")
			}
		})
	}
}

func TestEasyPayRSAQueryOrderGatewayContract(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	queryNonceCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse query form: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.PostForm.Get("gateway_sign_type") != signTypeRSA2 ||
			request.PostForm.Get("gateway_key_id") != "test-key-1" ||
			request.PostForm.Get("act") != "order" ||
			request.PostForm.Get("out_trade_no") != "order-42" {
			t.Errorf("unexpected RSA query params: %v", request.PostForm)
		}
		queryNonce := request.PostForm.Get("query_nonce")
		if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(queryNonce) {
			t.Errorf("invalid query nonce %q", queryNonce)
		}
		queryNonceCh <- queryNonce
		responseFields := map[string]string{
			"code": "1", "msg": "ok", "pid": "pid-1", "trade_no": "gateway-42",
			"out_trade_no": "order-42", "type": "wxpay", "money": "12.34",
			"status": "1", "trade_status": tradeStatusSuccess, "query_nonce": queryNonce,
			"key_id": "test-key-1", "timestamp": strconv.FormatInt(time.Now().Unix(), 10),
			"nonce_str": strings.Repeat("a", 32), "sign_type": signTypeRSA2,
		}
		easyPaySignRSAFields(t, responseFields, privateKey, "query")
		response := make(map[string]any, len(responseFields))
		for key, value := range responseFields {
			switch key {
			case "code", "status":
				integer, _ := strconv.Atoi(value)
				response[key] = integer
			default:
				response[key] = value
			}
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	provider := newEasyPayRSAProvider(t, server.URL, &privateKey.PublicKey)
	result, err := provider.QueryOrder(context.Background(), "order-42")
	if err != nil {
		t.Fatalf("QueryOrder rejected signed RSA2 response: %v", err)
	}
	if result.Status != payment.ProviderStatusPaid || result.TradeNo != "gateway-42" || result.Amount != 12.34 {
		t.Fatalf("unexpected query result: %+v", result)
	}
	if queryNonce := <-queryNonceCh; queryNonce == "" {
		t.Fatal("QueryOrder did not send a query nonce")
	}
}

func TestEasyPayRSAQueryRequiresConsistentPaymentStatus(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	provider := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
	tests := []struct {
		name        string
		tradeStatus string
		status      string
		tradeNo     string
		want        string
		wantError   bool
	}{
		{name: "paid pair", tradeStatus: tradeStatusSuccess, status: "1", tradeNo: "gateway-42", want: payment.ProviderStatusPaid},
		{name: "pending pair", tradeStatus: "WAITING", status: "0", tradeNo: "gateway-42", want: payment.ProviderStatusPending},
		{name: "success with unpaid status", tradeStatus: tradeStatusSuccess, status: "0", tradeNo: "gateway-42", wantError: true},
		{name: "waiting with paid status", tradeStatus: "WAITING", status: "1", tradeNo: "gateway-42", wantError: true},
		{name: "unknown status with paid integer", tradeStatus: "FAILED", status: "1", tradeNo: "gateway-42", wantError: true},
		{name: "failed status with unpaid integer", tradeStatus: "FAILED", status: "0", tradeNo: "gateway-42", wantError: true},
		{name: "paid without transaction id", tradeStatus: tradeStatusSuccess, status: "1", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := map[string]string{
				"code": "1", "msg": "ok", "pid": "pid-1", "trade_no": test.tradeNo,
				"out_trade_no": "order-42", "type": "wxpay", "money": "12.34",
				"status": test.status, "trade_status": test.tradeStatus,
				"query_nonce": "expected-query-nonce", "key_id": "test-key-1",
				"timestamp": strconv.FormatInt(time.Now().Unix(), 10),
				"nonce_str": strings.Repeat("a", 32), "sign_type": signTypeRSA2,
			}
			easyPaySignRSAFields(t, fields, privateKey, "query")
			response := make(map[string]any, len(fields))
			for key, value := range fields {
				switch key {
				case "code", "status":
					integer, _ := strconv.Atoi(value)
					response[key] = integer
				default:
					response[key] = value
				}
			}
			body, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			result, err := provider.parseRSAQueryResponse(body, "order-42", "expected-query-nonce")
			if test.wantError {
				if err == nil {
					t.Fatalf("invalid signed query response was accepted: %+v", result)
				}
				return
			}
			if err != nil {
				t.Fatalf("consistent status pair was rejected: %v", err)
			}
			if result.Status != test.want {
				t.Fatalf("status = %q, want %q", result.Status, test.want)
			}
		})
	}
}

func readEasyPayRSAContractVector(t *testing.T) easyPayRSAContractVector {
	t.Helper()
	path := os.Getenv("EASYPAY_RSA_VECTOR")
	if path == "" {
		path = filepath.Join("testdata", "easypay_rsa_contract.json")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read RSA contract vector: %v", err)
	}
	var vector easyPayRSAContractVector
	if err := json.Unmarshal(content, &vector); err != nil {
		t.Fatalf("parse RSA contract vector: %v", err)
	}
	return vector
}

func TestEasyPayRSACanonicalRejectsInvalidFieldsAndContext(t *testing.T) {
	if _, err := easyPayRSACanonical(map[string]string{"bad-key": "x"}, "notify"); err == nil {
		t.Fatal("canonicalizer accepted an invalid field name")
	}
	if _, err := easyPayRSACanonical(map[string]string{"field": "value"}, "unknown"); err == nil {
		t.Fatal("canonicalizer accepted an unknown signing purpose")
	}
}

func TestEasyPayRSASuccessResponseRejectsUnsignedAndAmbiguousQuery(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	provider := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
	fields := map[string]string{
		"code": "1", "msg": "ok", "pid": "pid-1", "trade_no": "gateway-42",
		"out_trade_no": "order-42", "type": "wxpay", "money": "12.34",
		"status": "1", "trade_status": tradeStatusSuccess, "query_nonce": "expected",
		"key_id": "test-key-1", "timestamp": strconv.FormatInt(time.Now().Unix(), 10),
		"nonce_str": strings.Repeat("a", 32), "sign_type": signTypeRSA2,
	}
	unsigned, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.parseRSAQueryResponse(unsigned, "order-42", "expected"); err == nil {
		t.Fatal("unsigned successful query response was accepted")
	}
	for _, body := range []string{
		`{"code":1,"status":1.0}`,
		`{"code":1,"status":1e0}`,
		`{"code":1,"status":"1"}`,
		`{"code":1,"status":{}}`,
		`{"code":1,"code":1}`,
	} {
		if _, err := easyPayDecodeFlatJSON([]byte(body)); err == nil {
			t.Fatalf("ambiguous or duplicate JSON field was accepted: %s", body)
		}
	}
}

func TestEasyPayRSAQueryResponseRejectsWrongIdentity(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	provider := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
	tests := []struct {
		name   string
		mutate func(map[string]string)
		order  string
		nonce  string
	}{
		{name: "wrong nonce", mutate: func(fields map[string]string) { fields["query_nonce"] = "wrong" }, order: "order-42", nonce: "expected"},
		{name: "wrong merchant", mutate: func(fields map[string]string) { fields["pid"] = "other-merchant" }, order: "order-42", nonce: "expected"},
		{name: "wrong order", mutate: func(fields map[string]string) { fields["out_trade_no"] = "another-order" }, order: "order-42", nonce: "expected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fields := map[string]string{
				"code": "1", "msg": "ok", "pid": "pid-1", "trade_no": "gateway-42",
				"out_trade_no": "order-42", "type": "wxpay", "money": "12.34",
				"status": "1", "trade_status": tradeStatusSuccess, "query_nonce": "expected",
				"key_id": "test-key-1", "timestamp": strconv.FormatInt(time.Now().Unix(), 10),
				"nonce_str": strings.Repeat("a", 32), "sign_type": signTypeRSA2,
			}
			test.mutate(fields)
			easyPaySignRSAFields(t, fields, privateKey, "query")
			body, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.parseRSAQueryResponse(body, test.order, test.nonce); err == nil {
				t.Fatal("query response with mismatched identity was accepted")
			}
		})
	}
}

func TestEasyPayRSAQueryDoesNotFallbackToMD5(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	var nonce string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse query form: %v", err)
		}
		nonce = request.PostForm.Get("query_nonce")
		_, _ = writer.Write([]byte(`{"code":1,"trade_status":"TRADE_SUCCESS","status":1,"money":"12.34","trade_no":"gateway-42","out_trade_no":"order-42"}`))
	}))
	defer server.Close()
	provider := newEasyPayRSAProvider(t, server.URL, &privateKey.PublicKey)
	if _, err := provider.QueryOrder(context.Background(), "order-42"); err == nil {
		t.Fatal("unsigned success response was accepted via MD5 fallback")
	}
	if nonce == "" {
		t.Fatal("RSA2 query did not send its nonce")
	}
}

func TestEasyPayRSAContractVectorHasNoPrivateKey(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "easypay_rsa_contract.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "PRIVATE KEY") {
		t.Fatal("contract vector must not contain a private key")
	}
}

func TestEasyPayLegacyMD5NotificationCompatibility(t *testing.T) {
	provider := newTestEasyPay(t, "http://gateway.invalid")
	params := map[string]string{
		"pid": "pid-1", "trade_no": "legacy-gateway-42", "out_trade_no": "legacy-order-42",
		"type": "wxpay", "name": "legacy recharge", "money": "12.34",
		"trade_status": tradeStatusSuccess,
	}
	params["sign"] = easyPaySign(params, provider.config["pkey"])
	params["sign_type"] = signTypeMD5

	notification, err := provider.VerifyNotification(context.Background(), encodeEasyPayRSAForm(params), nil)
	if err != nil {
		t.Fatalf("legacy MD5 notification rejected: %v", err)
	}
	if notification.Status != payment.ProviderStatusSuccess || notification.OrderID != "legacy-order-42" {
		t.Fatalf("unexpected legacy notification: %+v", notification)
	}
}
