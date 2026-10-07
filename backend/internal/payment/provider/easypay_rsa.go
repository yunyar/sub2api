package provider

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

var easyPayRSAKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var easyPayRSAFieldPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func easyPayValidKeyID(keyID string) bool {
	return easyPayRSAKeyPattern.MatchString(keyID)
}

func easyPayRandomNonce() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

func easyPayPublicKeyFingerprint(publicKey string) string {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return ""
	}
	fingerprint := sha256.Sum256([]byte(publicKey))
	return hex.EncodeToString(fingerprint[:])
}

func (e *EasyPay) rsaIdentityMetadata() map[string]string {
	return map[string]string{
		"gateway_sign_type":         signTypeRSA2,
		"gateway_key_id":            e.gatewayKeyID,
		"gateway_public_key_sha256": easyPayPublicKeyFingerprint(e.config["gatewayPublicKey"]),
	}
}

func (e *EasyPay) rsaNotifyURL(notifyURL string) (string, error) {
	if e.gatewaySignType != signTypeRSA2 {
		return notifyURL, nil
	}
	parsed, err := url.Parse(notifyURL)
	if err != nil {
		return "", fmt.Errorf("invalid easypay RSA notify URL: %w", err)
	}
	query := parsed.Query()
	query.Set("gateway_sign_type", signTypeRSA2)
	query.Set("gateway_key_id", e.gatewayKeyID)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func easyPayRSACanonical(params map[string]string, purpose string) ([]byte, error) {
	var prefix string
	switch purpose {
	case "notify":
		prefix = "sub2api-easypay-notify-v1\n"
	case "query":
		prefix = "sub2api-easypay-query-v1\n"
	default:
		return nil, fmt.Errorf("unsupported easypay RSA context")
	}
	keys := make([]string, 0, len(params))
	for key, value := range params {
		if !easyPayRSAFieldPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid signed field name")
		}
		if key != "sign" && key != "sign_type" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	canonical.WriteString(prefix)
	for index, key := range keys {
		if index > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(easyPayRFC3986(key))
		canonical.WriteByte('=')
		canonical.WriteString(easyPayRFC3986(params[key]))
	}
	return []byte(canonical.String()), nil
}

func easyPayRFC3986(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var result strings.Builder
	for _, b := range []byte(value) {
		if b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' ||
			b == '-' || b == '.' || b == '_' || b == '~' {
			result.WriteByte(b)
			continue
		}
		result.WriteByte('%')
		result.WriteByte(hexDigits[b>>4])
		result.WriteByte(hexDigits[b&15])
	}
	return result.String()
}

func parseEasyPayRSAPublicKey(value string) (*rsa.PublicKey, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, fmt.Errorf("invalid PEM public key")
	}
	var publicKey *rsa.PublicKey
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid PKIX public key")
		}
		var ok bool
		publicKey, ok = parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is not RSA")
		}
	case "RSA PUBLIC KEY":
		parsed, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid PKCS1 public key")
		}
		publicKey = parsed
	default:
		return nil, fmt.Errorf("PEM must contain an RSA public key")
	}
	if publicKey.N.BitLen() < 2048 || publicKey.N.BitLen() > 8192 {
		return nil, fmt.Errorf("RSA public key must be between 2048 and 8192 bits")
	}
	return publicKey, nil
}

func verifyEasyPayRSA(params map[string]string, publicKey *rsa.PublicKey, expectedKeyID, purpose string, now time.Time) error {
	if publicKey == nil {
		return fmt.Errorf("RSA public key is not configured")
	}
	if params["sign_type"] != signTypeRSA2 {
		return fmt.Errorf("invalid RSA sign_type")
	}
	if params["key_id"] != expectedKeyID || !easyPayValidKeyID(expectedKeyID) {
		return fmt.Errorf("invalid RSA key_id")
	}
	timestampText := params["timestamp"]
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil || strconv.FormatInt(timestamp, 10) != timestampText {
		return fmt.Errorf("invalid RSA timestamp")
	}
	if delta := now.Unix() - timestamp; delta < -300 || delta > 300 {
		return fmt.Errorf("RSA timestamp outside allowed window")
	}
	nonce := params["nonce_str"]
	if len(nonce) != 32 || strings.ToLower(nonce) != nonce {
		return fmt.Errorf("invalid RSA nonce_str")
	}
	if _, err := hex.DecodeString(nonce); err != nil {
		return fmt.Errorf("invalid RSA nonce_str")
	}
	signature, err := base64.StdEncoding.DecodeString(params["sign"])
	if err != nil {
		return fmt.Errorf("invalid RSA signature encoding")
	}
	canonical, err := easyPayRSACanonical(params, purpose)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return fmt.Errorf("invalid RSA signature")
	}
	return nil
}

func (e *EasyPay) parseRSAQueryResponse(body []byte, orderID, requestNonce string) (*payment.QueryOrderResponse, error) {
	params, err := easyPayDecodeFlatJSON(body)
	if err != nil {
		return nil, fmt.Errorf("easypay parse RSA query: %w", err)
	}
	code, err := easyPayJSONInteger(params, "code")
	if err != nil {
		return nil, fmt.Errorf("easypay query response missing or invalid code")
	}
	if code != easypayCodeSuccess {
		return nil, fmt.Errorf("easypay query failed: %s", params["msg"])
	}
	if err := verifyEasyPayRSA(params, e.gatewayPublicKey, e.gatewayKeyID, "query", time.Now()); err != nil {
		return nil, fmt.Errorf("easypay RSA query verification: %w", err)
	}
	if params["query_nonce"] != requestNonce ||
		params["pid"] != e.config["pid"] ||
		params["out_trade_no"] != orderID {
		return nil, fmt.Errorf("easypay RSA query identity mismatch")
	}
	if strings.TrimSpace(params["pid"]) == "" ||
		strings.TrimSpace(params["out_trade_no"]) == "" ||
		strings.TrimSpace(requestNonce) == "" {
		return nil, fmt.Errorf("easypay RSA query identity is incomplete")
	}
	amount, err := parseEasyPayAmount(params["money"])
	if err != nil {
		return nil, fmt.Errorf("easypay query returned invalid money")
	}
	statusValue, err := easyPayJSONInteger(params, "status")
	if err != nil {
		return nil, fmt.Errorf("easypay RSA query response missing or invalid status")
	}
	status := payment.ProviderStatusPending
	switch {
	case params["trade_status"] == tradeStatusSuccess && statusValue == easypayStatusPaid:
		status = payment.ProviderStatusPaid
	case params["trade_status"] == "WAITING" && statusValue == 0:
	default:
		return nil, fmt.Errorf("easypay RSA query returned conflicting or unknown status")
	}
	tradeNo := strings.TrimSpace(params["trade_no"])
	if status == payment.ProviderStatusPaid && (amount <= 0 || tradeNo == "") {
		return nil, fmt.Errorf("easypay query returned paid order without valid money and trade_no")
	}
	metadata := e.rsaIdentityMetadata()
	metadata["pid"] = params["pid"]
	metadata["out_trade_no"] = params["out_trade_no"]
	return &payment.QueryOrderResponse{
		TradeNo:           tradeNo,
		Status:            status,
		Amount:            amount,
		Metadata:          metadata,
		SignatureVerified: true,
	}, nil
}

func easyPayDecodeFlatJSON(body []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected flat JSON object")
	}
	result := make(map[string]string)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok || !easyPayRSAFieldPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid response field")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate response field")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		if key == "code" || key == "status" {
			number := strings.TrimSpace(string(value))
			integer, err := strconv.ParseInt(number, 10, 64)
			if err != nil || strconv.FormatInt(integer, 10) != number {
				return nil, fmt.Errorf("invalid integer field")
			}
			result[key] = strconv.FormatInt(integer, 10)
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return nil, fmt.Errorf("response values must be strings")
		}
		result[key] = text
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, fmt.Errorf("invalid response object")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return result, nil
}

func easyPayJSONInteger(params map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(params[key])
	return value, err
}

func parseEasyPayAmount(value string) (float64, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return 0, fmt.Errorf("empty or whitespace-padded amount")
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, fmt.Errorf("invalid amount")
	}
	return amount, nil
}

func validateEasyPayJSONFields(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	root, err := decoder.Token()
	if err != nil || root != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	if err := consumeEasyPayJSONValue(decoder, root); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func consumeEasyPayJSONValue(decoder *json.Decoder, token json.Token) error {
	delim, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delim {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || !easyPayRSAFieldPattern.MatchString(key) {
				return fmt.Errorf("invalid JSON field name")
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("duplicate JSON field")
			}
			keys[key] = struct{}{}
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := consumeEasyPayJSONValue(decoder, value); err != nil {
				return err
			}
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
			return fmt.Errorf("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := consumeEasyPayJSONValue(decoder, value); err != nil {
				return err
			}
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
			return fmt.Errorf("invalid JSON array")
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	return nil
}
