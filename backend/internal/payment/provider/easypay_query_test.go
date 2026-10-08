package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func TestEasyPayQueryOrderStatusMapping(t *testing.T) {
	t.Parallel()

	const orderID = "order-123"
	tests := []struct {
		name        string
		body        string
		statusCode  int
		wantError   bool
		wantStatus  string
		wantTradeNo string
		wantAmount  float64
	}{
		{
			name:        "PayPro legacy response accepts matching pid",
			body:        `{"code":1,"pid":"pid-1","trade_status":"TRADE_SUCCESS","status":1,"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantStatus:  payment.ProviderStatusPaid,
			wantTradeNo: "gateway-123",
			wantAmount:  12.34,
		},
		{
			name:      "PayPro legacy response rejects mismatched pid",
			body:      `{"code":1,"pid":"other-pid","trade_status":"TRADE_SUCCESS","status":1,"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "PayPro legacy response rejects empty pid",
			body:      `{"code":1,"pid":"","trade_status":"TRADE_SUCCESS","status":1,"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "waiting trade status conflicts with paid numeric status",
			body:      `{"code":1,"trade_status":"WAITING","status":1,"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "empty trade status conflicts with paid numeric status",
			body:      `{"code":1,"trade_status":"","status":1,"money":"12.34","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:        "PayPro legacy nested response accepts matching pid",
			body:        `{"code":1,"data":{"pid":"pid-1","trade_status":"TRADE_SUCCESS","status":1,"money":"9.99","trade_no":"data-456","out_trade_no":"order-123"}}`,
			wantStatus:  payment.ProviderStatusPaid,
			wantTradeNo: "data-456",
			wantAmount:  9.99,
		},
		{
			name:        "legacy numeric paid status remains compatible",
			body:        `{"code":1,"pid":"pid-1","status":1,"money":"3.21","trade_no":"gateway-legacy-123","out_trade_no":"order-123"}`,
			wantStatus:  payment.ProviderStatusPaid,
			wantTradeNo: "gateway-legacy-123",
			wantAmount:  3.21,
		},
		{
			name:        "legacy numeric non paid status is pending",
			body:        `{"code":1,"status":0,"money":"3.21"}`,
			wantStatus:  payment.ProviderStatusPending,
			wantTradeNo: "",
			wantAmount:  3.21,
		},
		{
			name:      "trade success conflicts with unpaid numeric status",
			body:      `{"code":1,"pid":"pid-1","trade_status":"TRADE_SUCCESS","status":0,"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "cross-level payment statuses conflict",
			body:      `{"code":1,"pid":"pid-1","trade_status":"TRADE_SUCCESS","data":{"status":0},"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "PayPro legacy response missing pid cannot authorize credit",
			body:      `{"code":1,"status":1,"money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "paid response missing order cannot authorize credit",
			body:      `{"code":1,"pid":"pid-1","status":1,"money":"12.34","trade_no":"gateway-123"}`,
			wantError: true,
		},
		{
			name:      "query failure is an error",
			body:      `{"code":0,"msg":"订单不存在"}`,
			wantError: true,
		},
		{
			name:      "missing code is an error",
			body:      `{}`,
			wantError: true,
		},
		{
			name:      "duplicate JSON key is an error",
			body:      `{"code":1,"code":0,"trade_status":"TRADE_SUCCESS","money":"12.34","trade_no":"gateway-123"}`,
			wantError: true,
		},
		{
			name:      "different returned order reference is an error",
			body:      `{"code":1,"trade_status":"TRADE_SUCCESS","money":"12.34","trade_no":"gateway-123","out_trade_no":"another-order"}`,
			wantError: true,
		},
		{
			name:      "paid response without transaction id is an error",
			body:      `{"code":1,"trade_status":"TRADE_SUCCESS","money":"12.34","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:      "non-finite paid amount is an error",
			body:      `{"code":1,"trade_status":"TRADE_SUCCESS","money":"NaN","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			wantError: true,
		},
		{
			name:       "HTTP error response cannot confirm payment",
			body:       `{"code":1,"trade_status":"TRADE_SUCCESS","money":"12.34","trade_no":"gateway-123","out_trade_no":"order-123"}`,
			statusCode: http.StatusInternalServerError,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotForm url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
				}
				if r.URL.Path != "/api.php" {
					t.Errorf("path = %q, want /api.php", r.URL.Path)
				}
				if err := r.ParseForm(); err != nil {
					t.Errorf("ParseForm: %v", err)
				}
				gotForm = make(url.Values, len(r.PostForm))
				for key, values := range r.PostForm {
					gotForm[key] = append([]string(nil), values...)
				}
				w.Header().Set("Content-Type", "application/json")
				if tt.statusCode != 0 {
					w.WriteHeader(tt.statusCode)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			provider := newTestEasyPay(t, server.URL)
			resp, err := provider.QueryOrder(context.Background(), orderID)
			if err != nil {
				if !tt.wantError {
					t.Fatalf("QueryOrder returned unexpected error: %v", err)
				}
				return
			}
			if tt.wantError {
				t.Fatal("QueryOrder returned no error")
			}
			if resp == nil {
				t.Fatal("QueryOrder returned nil response")
			}
			if resp.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q (response=%+v)", resp.Status, tt.wantStatus, resp)
			}
			if resp.TradeNo != tt.wantTradeNo {
				t.Fatalf("trade_no = %q, want %q", resp.TradeNo, tt.wantTradeNo)
			}
			if resp.Amount != tt.wantAmount {
				t.Fatalf("amount = %v, want %v", resp.Amount, tt.wantAmount)
			}
			for key, want := range map[string]string{
				"act":          "order",
				"pid":          "pid-1",
				"key":          "pkey-1",
				"out_trade_no": orderID,
			} {
				if got := gotForm.Get(key); got != want {
					t.Fatalf("form[%s] = %q, want %q (form=%v)", key, got, want, gotForm)
				}
			}
		})
	}
}
