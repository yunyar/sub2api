package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

type cnOfficialPricingSource struct {
	Name  string
	URL   string
	Parse func([]byte) (map[string]*LiteLLMModelPricing, error)
}

type cnOfficialPricingSnapshot struct {
	URL       string                          `json:"url"`
	Currency  string                          `json:"currency"`
	FetchedAt time.Time                       `json:"fetched_at"`
	Models    map[string]*LiteLLMModelPricing `json:"models"`
}

type cnOfficialPricingStatus struct {
	URL         string    `json:"url"`
	Currency    string    `json:"currency"`
	LastAttempt time.Time `json:"last_attempt"`
	LastSuccess time.Time `json:"last_success"`
	ModelCount  int       `json:"model_count"`
	Error       string    `json:"error,omitempty"`
	Stale       bool      `json:"stale"`
}

type cnOfficialPricingCache struct {
	Version int                                  `json:"version"`
	Sources map[string]cnOfficialPricingSnapshot `json:"sources"`
}

type officialPricingDocumentClient interface {
	FetchOfficialPricingDocument(context.Context, string) ([]byte, error)
}

func cnOfficialPricingSources() []cnOfficialPricingSource {
	return []cnOfficialPricingSource{
		{Name: "kimi", URL: "https://platform.kimi.ai/docs/pricing/chat", Parse: parseCNOfficialKimiPricing},
		{Name: "deepseek", URL: cnOfficialDeepSeekPricingSourceURL, Parse: parseCNOfficialDeepSeekDocument},
		{Name: "minimax", URL: cnOfficialMiniMaxPricingSourceURL, Parse: parseCNOfficialMiniMaxDocument},
		{Name: "zai", URL: "https://docs.z.ai/guides/overview/pricing.md", Parse: parseCNOfficialZAIPricing},
	}
}

func canonicalCNOfficialModel(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range []string{"models/", "moonshot/", "moonshotai/", "kimi/", "deepseek/", "minimax/"} {
		if rest, ok := strings.CutPrefix(model, prefix); ok {
			return rest
		}
	}
	return model
}

func cnOfficialModelBelongsToSource(source, model string) bool {
	switch source {
	case "kimi":
		return strings.HasPrefix(model, "kimi-") || strings.HasPrefix(model, "moonshot-")
	case "deepseek":
		return strings.HasPrefix(model, "deepseek-")
	case "minimax":
		return strings.HasPrefix(model, "minimax-")
	case "zai":
		return strings.HasPrefix(model, "glm-")
	default:
		return false
	}
}

func validateCNOfficialModels(source string, models map[string]*LiteLLMModelPricing) error {
	if len(models) == 0 || len(models) > 2000 {
		return fmt.Errorf("invalid official model count: %d", len(models))
	}
	for model, pricing := range models {
		if model != canonicalCNOfficialModel(model) || !cnOfficialModelBelongsToSource(source, model) {
			return fmt.Errorf("unexpected %s model: %q", source, model)
		}
		if pricing == nil || pricing.TokenPricingAbsent || pricing.Mode != "chat" {
			return fmt.Errorf("invalid official token pricing: %q", model)
		}
		values := []float64{
			pricing.InputCostPerToken, pricing.OutputCostPerToken,
			pricing.InputCostPerTokenPriority, pricing.OutputCostPerTokenPriority,
			pricing.CacheReadInputTokenCost, pricing.CacheCreationInputTokenCost,
			pricing.CacheReadInputTokenCostPriority, pricing.CacheCreationInputTokenCostPriority,
			pricing.CacheCreationInputTokenCostAbove1hr,
			pricing.LongContextInputCostMultiplier, pricing.LongContextOutputCostMultiplier,
		}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				return fmt.Errorf("invalid official price for %q", model)
			}
		}
		if pricing.InputCostPerToken <= 0 || pricing.OutputCostPerToken <= 0 {
			return fmt.Errorf("missing paid official input/output price for %q", model)
		}
		if pricing.LongContextInputTokenThreshold < 0 ||
			(pricing.LongContextInputTokenThreshold > 0 &&
				(pricing.LongContextInputCostMultiplier < 1 || pricing.LongContextOutputCostMultiplier < 1)) {
			return fmt.Errorf("unsupported official context tiers for %q", model)
		}
	}
	return nil
}

func (s *PricingService) cnOfficialRefreshInterval() time.Duration {
	if s == nil || s.cfg == nil {
		return 6 * time.Hour
	}
	hours := s.cfg.Pricing.CNOfficialUpdateIntervalHours
	if hours < 1 {
		hours = 6
	}
	return time.Duration(hours) * time.Hour
}

func (s *PricingService) refreshCNOfficialPricing(force bool) error {
	return s.refreshCNOfficialPricingFromSources(force, cnOfficialPricingSources())
}

func (s *PricingService) refreshCNOfficialPricingFromSources(force bool, sources []cnOfficialPricingSource) error {
	if s == nil || s.cfg == nil || !s.cfg.Pricing.CNOfficialEnabled || s.remoteClient == nil {
		return nil
	}
	s.cnOfficialMu.Lock()
	defer s.cnOfficialMu.Unlock()

	type outcome struct {
		source cnOfficialPricingSource
		models map[string]*LiteLLMModelPricing
		err    error
	}
	results := make(chan outcome, len(sources))
	var workers sync.WaitGroup
	for _, source := range sources {
		s.mu.RLock()
		snapshot := s.cnOfficialSnapshots[source.Name]
		s.mu.RUnlock()
		if !force && !snapshot.FetchedAt.IsZero() && time.Since(snapshot.FetchedAt) < s.cnOfficialRefreshInterval() {
			continue
		}
		workers.Add(1)
		go func(source cnOfficialPricingSource) {
			defer workers.Done()
			result := outcome{source: source}
			sourceURL, err := s.validatePricingURL(source.URL)
			if err != nil {
				result.err = err
				results <- result
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var body []byte
			if client, ok := s.remoteClient.(officialPricingDocumentClient); ok {
				body, err = client.FetchOfficialPricingDocument(ctx, sourceURL)
			} else {
				body, err = s.remoteClient.FetchPricingJSON(ctx, sourceURL)
			}
			if err == nil && len(body) > 8<<20 {
				err = fmt.Errorf("official pricing document exceeds 8 MiB")
			}
			if err == nil {
				result.models, err = source.Parse(body)
			}
			if err == nil {
				err = validateCNOfficialModels(source.Name, result.models)
			}
			result.err = err
			results <- result
		}(source)
	}
	workers.Wait()
	close(results)
	changed := false
	var failures []error
	for result := range results {
		now := time.Now().UTC()
		s.mu.Lock()
		if s.cnOfficialStatus == nil {
			s.cnOfficialStatus = make(map[string]cnOfficialPricingStatus)
		}
		status := s.cnOfficialStatus[result.source.Name]
		status.URL = result.source.URL
		status.Currency = "USD"
		status.LastAttempt = now
		if result.err != nil {
			status.Error = result.err.Error()
		} else {
			if s.cnOfficialSnapshots == nil {
				s.cnOfficialSnapshots = make(map[string]cnOfficialPricingSnapshot)
			}
			s.cnOfficialSnapshots[result.source.Name] = cnOfficialPricingSnapshot{
				URL: result.source.URL, Currency: "USD", FetchedAt: now, Models: result.models,
			}
			status.LastSuccess = now
			status.ModelCount = len(result.models)
			status.Error = ""
			changed = true
		}
		s.cnOfficialStatus[result.source.Name] = status
		s.mu.Unlock()
		if result.err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Official %s refresh failed, retaining last good prices: %v", result.source.Name, result.err)
			failures = append(failures, fmt.Errorf("%s official pricing: %w", result.source.Name, result.err))
		} else {
			logger.LegacyPrintf("service.pricing", "[Pricing] Refreshed %d official %s USD prices", len(result.models), result.source.Name)
		}
	}
	if changed {
		s.rebuildCNOfficialPricing()
		if err := s.saveCNOfficialPricingCache(); err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Saving official pricing cache failed: %v", err)
		}
	}
	return errors.Join(failures...)
}

func (s *PricingService) rebuildCNOfficialPricing() {
	if s == nil || s.cfg == nil || !s.cfg.Pricing.CNOfficialEnabled {
		return
	}
	if err := s.validateCustomPricingFiles(); err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Retaining official prices because custom pricing validation failed: %v", err)
		return
	}
	s.mu.RLock()
	prices := make(map[string]*LiteLLMModelPricing)
	for _, snapshot := range s.cnOfficialSnapshots {
		for model, pricing := range snapshot.Models {
			prices[model] = pricing
		}
	}
	s.mu.RUnlock()
	if len(prices) == 0 {
		return
	}
	body, err := json.Marshal(prices)
	if err != nil {
		return
	}
	parsed, err := s.parsePricingData(body)
	if err != nil {
		logger.LegacyPrintf("service.pricing", "[Pricing] Official pricing override merge failed: %v", err)
		return
	}
	for model, pricing := range parsed {
		if _, official := prices[model]; !official {
			delete(parsed, model)
			continue
		}
		pricing.OfficialPricing = true
	}
	s.mu.Lock()
	s.cnOfficialData = parsed
	s.mu.Unlock()
}

func (s *PricingService) saveCNOfficialPricingCache() error {
	s.mu.RLock()
	body, err := json.Marshal(cnOfficialPricingCache{Version: 1, Sources: s.cnOfficialSnapshots})
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.cfg.Pricing.DataDir, ".cn-official-pricing-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(body); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(s.cfg.Pricing.DataDir, "cn_official_pricing.json"))
}

func (s *PricingService) loadCNOfficialPricingCache() {
	path := filepath.Join(s.cfg.Pricing.DataDir, "cn_official_pricing.json")
	body, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.LegacyPrintf("service.pricing", "[Pricing] Reading official pricing cache failed: %v", err)
		}
		return
	}
	var cache cnOfficialPricingCache
	if len(body) > 8<<20 || json.Unmarshal(body, &cache) != nil || cache.Version != 1 {
		logger.LegacyPrintf("service.pricing", "[Pricing] Invalid official pricing cache, ignoring")
		return
	}
	snapshots := make(map[string]cnOfficialPricingSnapshot)
	statuses := make(map[string]cnOfficialPricingStatus)
	for _, source := range cnOfficialPricingSources() {
		snapshot, ok := cache.Sources[source.Name]
		if !ok {
			continue
		}
		if snapshot.URL != source.URL || snapshot.Currency != "USD" || snapshot.FetchedAt.IsZero() ||
			snapshot.FetchedAt.After(time.Now().Add(5*time.Minute)) {
			continue
		}
		if err := validateCNOfficialModels(source.Name, snapshot.Models); err != nil {
			logger.LegacyPrintf("service.pricing", "[Pricing] Invalid official %s cache: %v", source.Name, err)
			continue
		}
		snapshots[source.Name] = snapshot
		statuses[source.Name] = cnOfficialPricingStatus{
			URL: source.URL, Currency: "USD", LastSuccess: snapshot.FetchedAt, ModelCount: len(snapshot.Models),
		}
	}
	s.mu.Lock()
	s.cnOfficialSnapshots = snapshots
	s.cnOfficialStatus = statuses
	s.mu.Unlock()
	s.rebuildCNOfficialPricing()
}

func (s *PricingService) cnOfficialPricingStatusLocked() map[string]any {
	sources := make(map[string]cnOfficialPricingStatus)
	for _, source := range cnOfficialPricingSources() {
		status := s.cnOfficialStatus[source.Name]
		status.URL = source.URL
		status.Currency = "USD"
		status.Stale = status.LastSuccess.IsZero() || time.Since(status.LastSuccess) > 3*s.cnOfficialRefreshInterval()
		sources[source.Name] = status
	}
	models := make([]string, 0, len(s.cnOfficialData))
	for model := range s.cnOfficialData {
		models = append(models, model)
	}
	sort.Strings(models)
	return map[string]any{
		"enabled":     s.cfg != nil && s.cfg.Pricing.CNOfficialEnabled,
		"sources":     sources,
		"model_count": len(models),
		"models":      models,
	}
}
