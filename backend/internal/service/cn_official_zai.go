package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func parseCNOfficialZAIPricing(body []byte) (map[string]*LiteLLMModelPricing, error) {
	text := string(body)
	if !strings.Contains(text, "All prices are in USD.") {
		return nil, fmt.Errorf("Z.AI pricing document does not declare USD")
	}
	models := make(map[string]*LiteLLMModelPricing)
	unitConfirmed, table := false, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			unitConfirmed, table = false, false
			continue
		}
		if line == "Prices per 1M tokens." {
			unitConfirmed = true
			continue
		}
		if !strings.HasPrefix(line, "|") {
			table = false
			continue
		}
		fields := strings.Split(strings.Trim(line, "|"), "|")
		for index := range fields {
			fields[index] = strings.TrimSpace(fields[index])
		}
		if len(fields) > 0 && fields[0] == "Model" {
			table = unitConfirmed && strings.Join(fields, "|") == "Model|Input|Cached Input|Cached Input Storage|Output"
			if unitConfirmed && !table {
				return nil, fmt.Errorf("unsupported Z.AI token pricing columns")
			}
			continue
		}
		if !table || len(fields) == 0 || strings.HasPrefix(fields[0], ":") || strings.HasPrefix(fields[0], "-") {
			continue
		}
		if len(fields) != 5 {
			return nil, fmt.Errorf("invalid Z.AI token pricing row")
		}
		model := strings.ToLower(fields[0])
		if !strings.HasPrefix(model, "glm-") || strings.ContainsAny(model, " /<>[]*") {
			return nil, fmt.Errorf("invalid Z.AI model ID: %q", fields[0])
		}
		values := make([]float64, 3)
		for index, column := range []int{1, 2, 4} {
			value := strings.TrimPrefix(fields[column], `\`)
			if !strings.HasPrefix(value, "$") {
				return nil, fmt.Errorf("Z.AI token price is not a USD amount: %q", value)
			}
			amount, err := strconv.ParseFloat(strings.TrimPrefix(value, "$"), 64)
			if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
				return nil, fmt.Errorf("invalid Z.AI token price: %q", value)
			}
			values[index] = amount / 1_000_000
		}
		if fields[3] != "Limited-time Free" && fields[3] != "Free" {
			return nil, fmt.Errorf("unsupported Z.AI cache storage pricing: %q", fields[3])
		}
		if _, duplicate := models[model]; duplicate {
			return nil, fmt.Errorf("duplicate Z.AI model: %s", model)
		}
		models[model] = &LiteLLMModelPricing{
			InputCostPerToken: values[0], CacheReadInputTokenCost: values[1], OutputCostPerToken: values[2],
			LiteLLMProvider: "z_ai", Mode: "chat", SupportsPromptCaching: true,
		}
	}
	if err := validateCNOfficialModels("zai", models); err != nil {
		return nil, err
	}
	return models, nil
}
