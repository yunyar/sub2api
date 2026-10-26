//go:build unit

package service

import (
	"os"
	"strings"
	"testing"
)

func TestParseCNOfficialMiniMaxPricing(t *testing.T) {
	body, err := os.ReadFile("testdata/cn_official_minimax.md")
	if err != nil {
		t.Fatal(err)
	}

	models, err := parseCNOfficialMiniMaxPricing(body)
	if err != nil {
		t.Fatal(err)
	}
	m3 := models["minimax-m3"]
	if m3 == nil {
		t.Fatal("MiniMax M3 missing")
	}
	assertClosePrice(t, m3.InputCostPerToken, 0.60e-6)
	assertClosePrice(t, m3.OutputCostPerToken, 2.40e-6)
	assertClosePrice(t, m3.CacheReadInputTokenCost, 0.12e-6)
	if m3.LongContextInputTokenThreshold != 512_000 {
		t.Fatalf("long-context threshold = %d, want 512000", m3.LongContextInputTokenThreshold)
	}
	assertClosePrice(t, m3.LongContextInputCostMultiplier, 2)
	assertClosePrice(t, m3.LongContextOutputCostMultiplier, 2)
	if !m3.SupportsPromptCaching || m3.Mode != "chat" {
		t.Fatalf("unexpected MiniMax M3 metadata: %+v", m3)
	}

	m27 := models["minimax-m2.7"]
	if m27 == nil {
		t.Fatal("MiniMax M2.7 missing")
	}
	assertClosePrice(t, m27.InputCostPerToken, 0.30e-6)
	assertClosePrice(t, m27.OutputCostPerToken, 1.20e-6)
	assertClosePrice(t, m27.CacheReadInputTokenCost, 0.06e-6)
	if m27.LongContextInputTokenThreshold != 0 {
		t.Fatalf("single context-price row was incorrectly treated as tiered: %+v", m27)
	}
}

func TestParseCNOfficialMiniMaxPricingUsesExactDynamicIDs(t *testing.T) {
	body, err := os.ReadFile("testdata/cn_official_minimax.md")
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.ReplaceAll(string(body), "MiniMax-M2.7", "MiniMax-M2.9-preview"))

	models, err := parseCNOfficialMiniMaxPricing(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := models["minimax-m2.9-preview"]; !ok {
		t.Fatalf("new exact model ID was not parsed: %v", models)
	}
	if _, ok := models["minimax-m2"]; ok {
		t.Fatalf("parser synthesized a family fallback: %v", models)
	}
}

func TestParseCNOfficialMiniMaxPricingRejectsMalformedOrChangedSource(t *testing.T) {
	body, err := os.ReadFile("testdata/cn_official_minimax.md")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		body []byte
	}{
		{name: "empty", body: nil},
		{name: "implicit currency", body: []byte(strings.ReplaceAll(string(body), "USD", "currency"))},
		{name: "invalid number", body: []byte(strings.Replace(string(body), "$0.60", "$NaN", 1))},
		{name: "unsupported context tier", body: []byte(strings.Replace(string(body), ">512K", ">1M", 1))},
		{name: "missing cache amount", body: []byte(strings.Replace(string(body), "$0.12", "-", 1))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseCNOfficialMiniMaxPricing(test.body); err == nil {
				t.Fatal("expected malformed or changed source to fail")
			}
		})
	}
}

func TestCNOfficialMiniMaxSourceURL(t *testing.T) {
	if cnOfficialMiniMaxPricingSourceURL != "https://platform.minimax.io/docs/guides/pricing-paygo.md" {
		t.Fatalf("unexpected official MiniMax source URL: %s", cnOfficialMiniMaxPricingSourceURL)
	}
}

func assertOfficialPrice(t *testing.T, models map[string]*LiteLLMModelPricing, model string, input, output, cacheRead float64) {
	t.Helper()
	pricing := models[model]
	if pricing == nil {
		t.Fatalf("pricing for %s missing: %v", model, models)
	}
	assertClosePrice(t, pricing.InputCostPerToken, input)
	assertClosePrice(t, pricing.OutputCostPerToken, output)
	assertClosePrice(t, pricing.CacheReadInputTokenCost, cacheRead)
}

func assertClosePrice(t *testing.T, actual, expected float64) {
	t.Helper()
	delta := actual - expected
	if delta < -1e-15 || delta > 1e-15 {
		t.Fatalf("price = %.12g, want %.12g", actual, expected)
	}
}
