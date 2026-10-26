//go:build unit

package service

import (
	"os"
	"strings"
	"testing"
)

func TestParseCNOfficialDeepSeekPricing(t *testing.T) {
	body, err := os.ReadFile("testdata/cn_official_deepseek.html")
	if err != nil {
		t.Fatal(err)
	}

	models, err := parseCNOfficialDeepSeekPricing(body)
	if err != nil {
		t.Fatal(err)
	}
	assertOfficialPrice(t, models, "deepseek-flash", 1.5e-7, 6e-7, 3e-9)
	assertOfficialPrice(t, models, "deepseek-v4-pro", 6.6e-7, 1.98e-6, 2.2e-8)
	if models["deepseek-flash"].Mode != "chat" || !models["deepseek-flash"].SupportsPromptCaching {
		t.Fatalf("unexpected DeepSeek pricing metadata: %+v", models["deepseek-flash"])
	}
}

func TestParseCNOfficialDeepSeekPricingUsesExactDynamicIDs(t *testing.T) {
	body, err := os.ReadFile("testdata/cn_official_deepseek.html")
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), "deepseek-v4-pro", "deepseek-v4-exact-20261026"))

	models, err := parseCNOfficialDeepSeekPricing(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := models["deepseek-v4-exact-20261026"]; !ok {
		t.Fatalf("new exact model ID was not parsed: %v", models)
	}
	if _, ok := models["deepseek-v4"]; ok {
		t.Fatalf("parser synthesized a family fallback: %v", models)
	}
}

func TestParseCNOfficialDeepSeekPricingRejectsMalformedOrChangedSource(t *testing.T) {
	body, err := os.ReadFile("testdata/cn_official_deepseek.html")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body []byte
	}{
		{name: "empty", body: nil},
		{name: "implicit currency", body: []byte(strings.ReplaceAll(string(body), "USD", "currency"))},
		{name: "price missing dollar unit", body: []byte(strings.Replace(string(body), "$0.003", "0.003", 1))},
		{name: "changed peak ratio", body: []byte(strings.Replace(string(body), "$0.006", "$0.007", 1))},
		{name: "unsupported input row", body: []byte(strings.Replace(string(body), "Cache Miss", "Cached", 1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseCNOfficialDeepSeekPricing(test.body); err == nil {
				t.Fatal("expected malformed or changed source to fail")
			}
		})
	}
}

func TestCNOfficialDeepSeekSourceURL(t *testing.T) {
	if cnOfficialDeepSeekPricingSourceURL != "https://api-docs.deepseek.com/quick_start/pricing" {
		t.Fatalf("unexpected official DeepSeek source URL: %s", cnOfficialDeepSeekPricingSourceURL)
	}
}
