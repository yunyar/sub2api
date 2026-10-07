package provider

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestEasyPayRejectsCreationSignatureDelimiterSmuggling(t *testing.T) {
	gateway := &EasyPay{config: map[string]string{"pid": "1000", "pkey": "test-merchant-secret"}}
	for _, includeTradeNo := range []bool{false, true} {
		name := "disclosed-payload"
		suffix := "&trade_status=TRADE_SUCCESS"
		if includeTradeNo {
			name = "with-transaction-id"
			suffix = "&trade_no=GATEWAY123" + suffix
		}
		t.Run(name, func(t *testing.T) {
			prefix := "https://site.example.com/payment/result?order_id=99&out_trade_no=ORDER123&status=success"
			creation := map[string]string{
				"pid": "1000", "type": "alipay", "out_trade_no": "ORDER123",
				"notify_url": "https://site.example.com/api/v1/payment/webhook/easypay",
				"return_url": prefix + suffix, "name": "balance recharge", "money": "650.00",
			}
			signature := easyPaySign(creation, gateway.config["pkey"])
			forged := cloneStringMap(creation)
			forged["return_url"] = prefix
			forged["trade_status"] = "TRADE_SUCCESS"
			if includeTradeNo {
				forged["trade_no"] = "GATEWAY123"
			}
			if easyPaySign(forged, gateway.config["pkey"]) != signature {
				t.Fatal("regression payload must reproduce the canonical signing collision")
			}
			forged["sign"] = signature
			forged["sign_type"] = signTypeMD5
			_, err := gateway.VerifyNotification(context.Background(), encodeEasyPayRSAForm(forged), nil)
			if err == nil || !strings.Contains(err.Error(), "unexpected notify field") {
				t.Fatalf("smuggled callback must fail allowlist before identity/signature checks: %v", err)
			}
			creation["sign"] = signature
			creation["sign_type"] = signTypeMD5
			if _, err := gateway.VerifyNotification(context.Background(), encodeEasyPayRSAForm(creation), nil); err == nil {
				t.Fatal("complete checkout URL replay accepted")
			}
		})
	}
}

func TestEasyPayNotificationAllowlistRejectsUnknownEvenEmpty(t *testing.T) {
	privateKey := generateEasyPayRSATestKey(t)
	for _, mode := range []string{signTypeMD5, signTypeRSA2} {
		t.Run(mode, func(t *testing.T) {
			gateway := newEasyPayRSAProvider(t, "http://gateway.invalid", &privateKey.PublicKey)
			fields := easyPayRSANotifyFields(time.Now().Unix())
			if mode == signTypeMD5 {
				gateway.gatewaySignType = signTypeMD5
				delete(fields, "timestamp")
				delete(fields, "key_id")
				delete(fields, "nonce_str")
				fields["sign_type"] = signTypeMD5
				fields["sign"] = easyPaySign(fields, gateway.config["pkey"])
			} else {
				easyPaySignRSAFields(t, fields, privateKey, "notify")
			}
			if _, err := gateway.VerifyNotification(context.Background(), encodeEasyPayRSAForm(fields), nil); err != nil {
				t.Fatalf("valid callback rejected: %v", err)
			}
			for _, unknown := range []string{"return_url", "notify_url", "device", "unexpected"} {
				for _, value := range []string{"", "anything"} {
					raw := encodeEasyPayRSAForm(fields) + "&" + unknown + "=" + url.QueryEscape(value)
					if _, err := gateway.VerifyNotification(context.Background(), raw, nil); err == nil {
						t.Fatalf("accepted unknown field %q with value %q", unknown, value)
					}
				}
			}
		})
	}
}
