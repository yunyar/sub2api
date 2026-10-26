package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type cnOfficialTestClient struct {
	mu    sync.Mutex
	body  []byte
	err   error
	calls int
}

func (client *cnOfficialTestClient) FetchPricingJSON(_ context.Context, _ string) ([]byte, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.calls++
	return client.body, client.err
}

func (client *cnOfficialTestClient) FetchHashText(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("unused")
}

func cnOfficialTestModels(inputPrice float64) map[string]*LiteLLMModelPricing {
	return map[string]*LiteLLMModelPricing{
		"kimi-k3.1-pro": {
			InputCostPerToken: inputPrice, OutputCostPerToken: 10e-6,
			CacheReadInputTokenCost:     0.2e-6,
			CacheCreationInputTokenCost: 2e-6, CacheCreationInputTokenCostAbove1hr: 4e-6,
			Mode: "chat", LiteLLMProvider: "moonshot", SupportsPromptCaching: true,
		},
	}
}

func newCNOfficialTestService(t *testing.T) (*PricingService, *cnOfficialTestClient, cnOfficialPricingSource) {
	t.Helper()
	body, err := json.Marshal(cnOfficialTestModels(2e-6))
	require.NoError(t, err)
	client := &cnOfficialTestClient{body: body}
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{
		DataDir: t.TempDir(), CNOfficialEnabled: true, CNOfficialUpdateIntervalHours: 6,
	}}, client)
	source := cnOfficialPricingSources()[0]
	source.Parse = func(body []byte) (map[string]*LiteLLMModelPricing, error) {
		var models map[string]*LiteLLMModelPricing
		err := json.Unmarshal(body, &models)
		return models, err
	}
	return svc, client, source
}

func TestCNOfficialRefreshUpdatesPricesAndRetainsLastGoodOnFailure(t *testing.T) {
	svc, client, source := newCNOfficialTestService(t)
	svc.refreshCNOfficialPricingFromSources(false, []cnOfficialPricingSource{source})
	require.InDelta(t, 2e-6, svc.GetModelPricing("kimi-k3.1-pro").InputCostPerToken, 1e-14)
	require.Equal(t, 1, client.calls)
	svc.refreshCNOfficialPricingFromSources(false, []cnOfficialPricingSource{source})
	require.Equal(t, 1, client.calls, "successful prices do not refetch before the interval")
	client.body = []byte(`{"kimi-k3.1-pro":{"input_cost_per_token":0.000003,"output_cost_per_token":0.000011,"mode":"chat","litellm_provider":"moonshot"}}`)
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	require.InDelta(t, 3e-6, svc.GetModelPricing("kimi-k3.1-pro").InputCostPerToken, 1e-14)
	client.err = fmt.Errorf("upstream unavailable")
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	require.InDelta(t, 3e-6, svc.GetModelPricing("kimi-k3.1-pro").InputCostPerToken, 1e-14)
	require.Contains(t, svc.cnOfficialStatus["kimi"].Error, "upstream unavailable")
	client.err = nil
	client.body = []byte(`{"kimi-k3.1-pro":{"input_cost_per_token":0,"output_cost_per_token":0,"mode":"chat"}}`)
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	require.InDelta(t, 3e-6, svc.GetModelPricing("kimi-k3.1-pro").InputCostPerToken, 1e-14)
}

func TestCNOfficialCacheRestartCurrencyAndSourceValidation(t *testing.T) {
	svc, _, source := newCNOfficialTestService(t)
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	restored := NewPricingService(svc.cfg, nil)
	restored.loadCNOfficialPricingCache()
	require.NotNil(t, restored.GetModelPricing("kimi-k3.1-pro"))
	require.InDelta(t, 4e-6, restored.GetModelPricing("kimi-k3.1-pro").CacheCreationInputTokenCostAbove1hr, 1e-14)
	for _, change := range []func(*cnOfficialPricingSnapshot){
		func(snapshot *cnOfficialPricingSnapshot) { snapshot.Currency = "CNY" },
		func(snapshot *cnOfficialPricingSnapshot) { snapshot.URL = "https://example.com/prices" },
		func(snapshot *cnOfficialPricingSnapshot) { snapshot.FetchedAt = time.Now().Add(24 * time.Hour) },
	} {
		snapshot := svc.cnOfficialSnapshots["kimi"]
		change(&snapshot)
		body, err := json.Marshal(cnOfficialPricingCache{Version: 1, Sources: map[string]cnOfficialPricingSnapshot{"kimi": snapshot}})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(svc.cfg.Pricing.DataDir, "cn_official_pricing.json"), body, 0600))
		rejected := NewPricingService(svc.cfg, nil)
		rejected.loadCNOfficialPricingCache()
		require.Nil(t, rejected.GetModelPricing("kimi-k3.1-pro"))
	}
}

func TestCNOfficialConcurrentRefreshAndBillingReads(t *testing.T) {
	svc, _, source := newCNOfficialTestService(t)
	require.NoError(t, svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source}))
	var readers sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for iteration := 0; iteration < 20; iteration++ {
				pricing := svc.GetIdentifiedModelPricing("moonshot/kimi-k3.1-pro")
				if pricing == nil || !pricing.OfficialPricing || pricing.InputCostPerToken != 2e-6 {
					t.Error("official pricing unavailable during refresh")
					return
				}
				svc.GetStatus()
				svc.ListModelNamesByProvider("moonshot")
			}
		}()
	}
	for iteration := 0; iteration < 5; iteration++ {
		require.NoError(t, svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source}))
	}
	readers.Wait()
}

func TestCNOfficialPricingOverridesCatalogButHonorsExplicitGroupAndFilePrices(t *testing.T) {
	svc, _, source := newCNOfficialTestService(t)
	svc.pricingData = map[string]*LiteLLMModelPricing{
		"kimi-k3.1-pro": {InputCostPerToken: 90e-6, OutputCostPerToken: 100e-6},
	}
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	require.InDelta(t, 2e-6, svc.GetIdentifiedModelPricing("moonshot/kimi-k3.1-pro").InputCostPerToken, 1e-14)
	require.Nil(t, svc.GetModelPricing("kimi-k3.10-pro"))
	billing := NewBillingService(&config.Config{}, svc)
	cost, err := billing.CalculateCost("kimi-k3.1-pro", UsageTokens{
		InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000,
		CacheCreation5mTokens: 1000, CacheCreation1hTokens: 1000, CacheCreationTokens: 2000,
	}, 1.5)
	require.NoError(t, err)
	require.InDelta(t, 0.0182, cost.TotalCost, 1e-12)
	require.InDelta(t, 0.0273, cost.ActualCost, 1e-12)
	manual := 7e-6
	group := &Group{ID: 28, ModelPricing: []ChannelModelPricing{{
		Models: []string{"kimi-k3.1-pro"}, BillingMode: BillingModeToken, InputPrice: &manual,
	}}}
	resolver := NewModelPricingResolver(nil, billing)
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "kimi-k3.1-pro", Group: group})
	require.InDelta(t, manual, resolved.BasePricing.InputPricePerToken, 1e-14)
	svc.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "override.json")
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.OverrideFile, []byte(`{"kimi-k3.1-pro":{"input_cost_per_token":0}}`), 0600))
	svc.rebuildCNOfficialPricing()
	require.Zero(t, svc.GetModelPricing("kimi-k3.1-pro").InputCostPerToken, "explicit operator free pricing remains explicit")
	require.InDelta(t, 10e-6, svc.GetModelPricing("kimi-k3.1-pro").OutputCostPerToken, 1e-14)
}

func TestCNOfficialSnapshotsProviderIsolationAndAllowlistFailure(t *testing.T) {
	svc, _, source := newCNOfficialTestService(t)
	svc.cfg.Security.URLAllowlist.Enabled = true
	svc.cfg.Security.URLAllowlist.PricingHosts = []string{"raw.githubusercontent.com"}
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	require.Empty(t, svc.cnOfficialData)
	require.NotEmpty(t, svc.cnOfficialStatus["kimi"].Error)
	svc.cfg.Security.URLAllowlist.PricingHosts = []string{"platform.kimi.ai"}
	svc.refreshCNOfficialPricingFromSources(true, []cnOfficialPricingSource{source})
	require.NotNil(t, svc.GetModelPricing("kimi-k3.1-pro"))
	require.Error(t, validateCNOfficialModels("deepseek", cnOfficialTestModels(2e-6)))
	status := svc.GetStatus()["cn_official"].(map[string]any)
	require.Equal(t, true, status["enabled"])
	require.Equal(t, 1, status["model_count"])
}

func TestCNOfficialDeepSeekPricesSurviveLegacyHardcodedPolicy(t *testing.T) {
	svc, _, _ := newCNOfficialTestService(t)
	pricing := &LiteLLMModelPricing{
		InputCostPerToken: 8e-6, OutputCostPerToken: 12e-6, CacheReadInputTokenCost: 1e-6,
		Mode: "chat", LiteLLMProvider: "deepseek",
	}
	svc.cnOfficialSnapshots = map[string]cnOfficialPricingSnapshot{
		"deepseek": {Models: map[string]*LiteLLMModelPricing{"deepseek-v4.1-flash": pricing}},
	}
	svc.rebuildCNOfficialPricing()
	billing := NewBillingService(&config.Config{}, svc)
	model, err := billing.GetModelPricing("deepseek-v4.1-flash")
	require.NoError(t, err)
	require.InDelta(t, 8e-6, model.InputPricePerToken, 1e-14)
	resolver := NewModelPricingResolver(nil, billing)
	for _, period := range []struct {
		at    time.Time
		scale float64
	}{
		{time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), 1},
		{time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC), 2},
	} {
		cost, err := billing.CalculateCostUnified(CostInput{
			Ctx: context.Background(), Model: "deepseek-v4.1-flash", Resolver: resolver,
			Tokens:         UsageTokens{InputTokens: 1000, OutputTokens: 1000, CacheReadTokens: 1000},
			RateMultiplier: 1.5, PricingAt: period.at,
		})
		require.NoError(t, err)
		require.InDelta(t, .021*period.scale, cost.TotalCost, 1e-12)
		require.InDelta(t, .0315*period.scale, cost.ActualCost, 1e-12)
	}
}

func TestCNOfficialOfflineKimiFallbacksUseExactPublishedModels(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	for _, expected := range []struct {
		model                string
		input, output, cache float64
	}{
		{"kimi-k3.1-pro", 2e-6, 10e-6, .2e-6},
		{"kimi-k3.1-pro-highspeed", 4e-6, 20e-6, .2e-6},
		{"kimi-k2.7-code", .95e-6, 4e-6, .19e-6},
		{"moonshotai/kimi-k2.7-code-highspeed", 1.9e-6, 8e-6, .38e-6},
	} {
		pricing, err := billing.GetModelPricing(expected.model)
		require.NoError(t, err)
		require.InDelta(t, expected.input, pricing.InputPricePerToken, 1e-14)
		require.InDelta(t, expected.output, pricing.OutputPricePerToken, 1e-14)
		require.InDelta(t, expected.cache, pricing.CacheReadPricePerToken, 1e-14)
	}
	_, err := billing.GetModelPricing("kimi-k3.10-pro")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestCNOfficialCapturedProviderDocuments(t *testing.T) {
	dir := os.Getenv("SUB2API_CN_OFFICIAL_DOCUMENT_DIR")
	if dir == "" {
		t.Skip("set SUB2API_CN_OFFICIAL_DOCUMENT_DIR to verify captured official documents")
	}
	for _, source := range cnOfficialPricingSources() {
		t.Run(source.Name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, source.Name+".txt"))
			require.NoError(t, err)
			models, err := source.Parse(body)
			require.NoError(t, err)
			require.NoError(t, validateCNOfficialModels(source.Name, models))
			t.Logf("%s: parsed %d official USD model prices", source.URL, len(models))
		})
	}
}
