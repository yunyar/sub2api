//go:build unit

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCNOfficialKimiPricingCompiledSource(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "cn_official_kimi_compiled.html"))
	require.NoError(t, err)

	pricing, err := parseCNOfficialKimiPricing(body)
	require.NoError(t, err)
	require.Len(t, pricing, 6)

	k31 := pricing["kimi-k3.1-pro"]
	require.NotNil(t, k31)
	require.InDelta(t, 2e-6, k31.InputCostPerToken, 1e-15)
	require.InDelta(t, 10e-6, k31.OutputCostPerToken, 1e-15)
	require.InDelta(t, 0.2e-6, k31.CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 2e-6, k31.CacheCreationInputTokenCost, 1e-15)
	require.InDelta(t, 4e-6, k31.CacheCreationInputTokenCostAbove1hr, 1e-15)
	require.True(t, k31.CacheCreationInputTokenCostExplicit)
	require.True(t, k31.SupportsPromptCaching)
	require.Equal(t, "moonshot", k31.LiteLLMProvider)
	require.Equal(t, "chat", k31.Mode)

	highspeed := pricing["kimi-k3.1-pro-highspeed"]
	require.NotNil(t, highspeed)
	require.InDelta(t, 4e-6, highspeed.InputCostPerToken, 1e-15)
	require.InDelta(t, 20e-6, highspeed.OutputCostPerToken, 1e-15)
	require.InDelta(t, 0.2e-6, highspeed.CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 4e-6, highspeed.CacheCreationInputTokenCost, 1e-15)
	require.InDelta(t, 8e-6, highspeed.CacheCreationInputTokenCostAbove1hr, 1e-15)

	k27 := pricing["kimi-k2.7-code"]
	require.NotNil(t, k27)
	require.InDelta(t, 0.95e-6, k27.InputCostPerToken, 1e-15)
	require.InDelta(t, 4e-6, k27.OutputCostPerToken, 1e-15)
	require.InDelta(t, 0.19e-6, k27.CacheReadInputTokenCost, 1e-15)
	require.Zero(t, k27.CacheCreationInputTokenCost)
	require.Zero(t, k27.CacheCreationInputTokenCostAbove1hr)
	require.False(t, k27.CacheCreationInputTokenCostExplicit)

	require.Contains(t, pricing, "kimi-k2.7-code-highspeed")
	require.Contains(t, pricing, "kimi-k3")
}

func TestParseCNOfficialKimiPricingNativeShiftedColumns(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "cn_official_kimi_native_shifted.html"))
	require.NoError(t, err)

	pricing, err := parseCNOfficialKimiPricing(body)
	require.NoError(t, err)
	require.Len(t, pricing, 1)
	got := pricing["kimi-k3.1-pro"]
	require.NotNil(t, got)
	require.InDelta(t, 2e-6, got.InputCostPerToken, 1e-15)
	require.InDelta(t, 10e-6, got.OutputCostPerToken, 1e-15)
	require.InDelta(t, 0.2e-6, got.CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 2e-6, got.CacheCreationInputTokenCost, 1e-15)
	require.InDelta(t, 4e-6, got.CacheCreationInputTokenCostAbove1hr, 1e-15)
}

func TestParseCNOfficialKimiPricingFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "empty",
			body: "",
		},
		{
			name: "no pricing table",
			body: "<html><body><p>Kimi pricing</p></body></html>",
		},
		{
			name: "missing cached input column",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output"},
				[]string{"kimi-k3.1-pro", "$2.00 / MTok", "$10.00 / MTok"},
			),
		},
		{
			name: "missing price cell",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input"},
				[]string{"kimi-k3.1-pro", "$2.00 / MTok", "$10.00 / MTok", ""},
			),
		},
		{
			name: "malformed price",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input"},
				[]string{"kimi-k3.1-pro", "$-2.00 / MTok", "$10.00 / MTok", "$0.20 / MTok"},
			),
		},
		{
			name: "free price without numeric amount",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input"},
				[]string{"kimi-k3.1-pro", "$2.00 / MTok", "$10.00 / MTok", "Free"},
			),
		},
		{
			name: "missing one write tier",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input", "5m writes"},
				[]string{"kimi-k3.1-pro", "$2.00 / MTok", "$10.00 / MTok", "$0.20 / MTok", "$2.00 / MTok"},
			),
		},
		{
			name: "duplicate price column",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input", "Cached Input"},
				[]string{"kimi-k3.1-pro", "$2.00 / MTok", "$10.00 / MTok", "$0.20 / MTok", "$0.20 / MTok"},
			),
		},
		{
			name: "unknown price column",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input", "Discount"},
				[]string{"kimi-k3.1-pro", "$2.00 / MTok", "$10.00 / MTok", "$0.20 / MTok", "$0.20 / MTok"},
			),
		},
		{
			name: "duplicate model",
			body: `<table><tr><th>Model</th><th>Input</th><th>Output</th><th>Cached Input</th></tr>` +
				`<tr><td>kimi-k3.1-pro</td><td>$2.00 / MTok</td><td>$10.00 / MTok</td><td>$0.20 / MTok</td></tr>` +
				`<tr><td>kimi-k3.1-pro</td><td>$2.00 / MTok</td><td>$10.00 / MTok</td><td>$0.20 / MTok</td></tr></table>`,
		},
		{
			name: "overflow",
			body: kimiNativePricingTable(
				[]string{"Model", "Input", "Output", "Cached Input"},
				[]string{"kimi-k3.1-pro", "$" + strings.Repeat("9", 400) + " / MTok", "$10.00 / MTok", "$0.20 / MTok"},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pricing, err := parseCNOfficialKimiPricing([]byte(tt.body))
			require.Error(t, err)
			require.Nil(t, pricing)
		})
	}
}

func kimiNativePricingTable(headers, cells []string) string {
	html := "<table><tr>"
	for _, header := range headers {
		html += "<th>" + header + "</th>"
	}
	html += "</tr><tr>"
	for _, cell := range cells {
		html += "<td>" + cell + "</td>"
	}
	return html + "</tr></table>"
}
