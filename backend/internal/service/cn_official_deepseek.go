package service

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

const cnOfficialDeepSeekPricingSourceURL = "https://api-docs.deepseek.com/quick_start/pricing"

var (
	cnOfficialDeepSeekModelID = regexp.MustCompile(`(?i)^deepseek-[a-z0-9][a-z0-9._-]*$`)
)

type cnOfficialDeepSeekRates struct {
	offPeak  [3]float64
	peak     [3]float64
	seen     [3]bool
	seenPeak [3]bool
}

func parseCNOfficialDeepSeekPricing(body []byte) (map[string]*LiteLLMModelPricing, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("DeepSeek official pricing document is empty")
	}

	tables, documentText, err := parseCNOfficialTables(body)
	if err != nil {
		return nil, fmt.Errorf("parse DeepSeek official pricing document: %w", err)
	}
	if err := requireCNOfficialUSDPerMillion(documentText); err != nil {
		return nil, fmt.Errorf("DeepSeek official pricing: %w", err)
	}

	ratesByModel := make(map[string]*cnOfficialDeepSeekRates)
	for _, table := range tables {
		modelColumns := make(map[int]string)
		for _, row := range table {
			for index, cell := range row {
				model := strings.ToLower(strings.TrimSpace(cell))
				if cnOfficialDeepSeekModelID.MatchString(model) {
					modelColumns[index] = model
				}
			}
			if len(modelColumns) == 0 {
				continue
			}

			metric, peak, recognized := deepSeekPriceMetric(row, modelColumns)
			if !recognized {
				if deepSeekRowLooksPriced(row, modelColumns) {
					return nil, fmt.Errorf("DeepSeek pricing table has an unsupported priced row: %q", strings.Join(row, " | "))
				}
				continue
			}

			for column, model := range modelColumns {
				if column >= len(row) {
					return nil, fmt.Errorf("DeepSeek pricing row is missing model column %q", model)
				}
				value, err := parseCNOfficialUSDPrice(row[column], false)
				if err != nil {
					return nil, fmt.Errorf("DeepSeek %s price: %w", model, err)
				}
				rates := ratesByModel[model]
				if rates == nil {
					rates = &cnOfficialDeepSeekRates{}
					ratesByModel[model] = rates
				}
				values, seen := &rates.offPeak, &rates.seen
				if peak {
					values, seen = &rates.peak, &rates.seenPeak
				}
				if (*seen)[metric] {
					return nil, fmt.Errorf("duplicate DeepSeek price row for %s metric %d", model, metric)
				}
				(*values)[metric] = value / 1_000_000
				(*seen)[metric] = true
			}
		}
	}

	if len(ratesByModel) == 0 {
		return nil, fmt.Errorf("DeepSeek official pricing document contains no supported model pricing table")
	}
	models := make(map[string]*LiteLLMModelPricing, len(ratesByModel))
	for model, rates := range ratesByModel {
		for metric := range rates.seen {
			if !rates.seen[metric] || !rates.seenPeak[metric] {
				return nil, fmt.Errorf("DeepSeek pricing for %s is missing an off-peak or peak price", model)
			}
			if !closeOfficialPrice(rates.peak[metric], rates.offPeak[metric]*2) {
				return nil, fmt.Errorf("DeepSeek peak price for %s metric %d is not twice its off-peak price", model, metric)
			}
		}
		models[model] = &LiteLLMModelPricing{
			InputCostPerToken:       rates.offPeak[1],
			OutputCostPerToken:      rates.offPeak[2],
			CacheReadInputTokenCost: rates.offPeak[0],
			SupportsPromptCaching:   true,
			LiteLLMProvider:         "deepseek",
			Mode:                    "chat",
		}
	}
	return models, nil
}

func deepSeekPriceMetric(row []string, modelColumns map[int]string) (int, bool, bool) {
	var labels []string
	for index, cell := range row {
		if _, isModel := modelColumns[index]; isModel {
			continue
		}
		labels = append(labels, cell)
	}
	label := normalizeCNOfficialText(strings.Join(labels, " "))
	if !strings.Contains(label, "input") && !strings.Contains(label, "output") {
		return 0, false, false
	}

	metric := -1
	switch {
	case strings.Contains(label, "input") && (strings.Contains(label, "cachehit") || strings.Contains(label, "cachedinput")):
		metric = 0
	case strings.Contains(label, "input") && (strings.Contains(label, "cachemiss") || strings.Contains(label, "uncached")):
		metric = 1
	case strings.Contains(label, "output"):
		metric = 2
	}
	if metric < 0 {
		return 0, false, false
	}
	if strings.Contains(label, "offpeak") || strings.Contains(label, "off-peak") {
		return metric, false, true
	}
	if strings.Contains(label, "peak") {
		return metric, true, true
	}
	return 0, false, false
}

func deepSeekRowLooksPriced(row []string, modelColumns map[int]string) bool {
	var labels []string
	for index, cell := range row {
		if _, isModel := modelColumns[index]; isModel {
			continue
		}
		labels = append(labels, cell)
	}
	label := normalizeCNOfficialText(strings.Join(labels, " "))
	return strings.Contains(label, "input") || strings.Contains(label, "output")
}

func closeOfficialPrice(actual, expected float64) bool {
	return !math.IsNaN(actual) && !math.IsInf(actual, 0) &&
		math.Abs(actual-expected) <= math.Max(1e-15, math.Abs(expected)*1e-9)
}
