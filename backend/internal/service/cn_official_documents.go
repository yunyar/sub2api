package service

import (
	"fmt"
	stdhtml "html"
	"regexp"
	"sort"
	"strings"
)

var cnOfficialFootnote = regexp.MustCompile(`\s*\([0-9]+\)\s*$`)
var cnOfficialMiniMaxPublishedModel = regexp.MustCompile(`^\*\*(MiniMax-[A-Za-z0-9._-]+)\*\*(.*)$`)
var cnOfficialMiniMaxPublishedAmount = regexp.MustCompile(`^(?:~~\\?\$[0-9]+(?:\.[0-9]+)?~~\s*)?(\\?\$[0-9]+(?:\.[0-9]+)?)$`)

func parseCNOfficialDeepSeekDocument(body []byte) (map[string]*LiteLLMModelPricing, error) {
	tables, text, err := parseCNOfficialTables(body)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(text, "Models & Pricing | DeepSeek API Docs") {
		return parseCNOfficialDeepSeekPricing(body)
	}
	if strings.ContainsAny(text, "¥￥") || strings.Contains(text, "CNY") || strings.Contains(text, "RMB") {
		return nil, fmt.Errorf("DeepSeek document currency does not match the official English USD page")
	}
	if !strings.Contains(normalizeCNOfficialText(text), "peakhoursare01:0004:00and06:0010:00utc,mondaythroughfriday") {
		return nil, fmt.Errorf("DeepSeek published peak schedule changed")
	}
	var normalized strings.Builder
	_, _ = normalized.WriteString("<p>USD per 1M tokens</p>")
	normalizedRow := func(row []string) {
		_, _ = normalized.WriteString("<tr>")
		for _, cell := range row {
			_, _ = normalized.WriteString("<td>" + stdhtml.EscapeString(cell) + "</td>")
		}
		_, _ = normalized.WriteString("</tr>")
	}
	for _, table := range tables {
		columns := make(map[int]string)
		var columnOrder []int
		for _, row := range table {
			isModelHeader := false
			for _, cell := range row {
				if strings.EqualFold(strings.TrimSpace(cell), "MODEL") {
					isModelHeader = true
				}
			}
			if !isModelHeader {
				continue
			}
			for index, cell := range row {
				model := strings.ToLower(cnOfficialFootnote.ReplaceAllString(strings.TrimSpace(cell), ""))
				if cnOfficialDeepSeekModelID.MatchString(model) {
					columns[index] = model
					columnOrder = append(columnOrder, index)
				}
			}
			break
		}
		if len(columns) == 0 {
			continue
		}
		_, _ = normalized.WriteString("<table>")
		header := []string{"Price item", "Time period"}
		for _, index := range columnOrder {
			header = append(header, columns[index])
		}
		normalizedRow(header)
		for _, row := range table {
			metric, peak, recognized := deepSeekPriceMetric(row, columns)
			if !recognized {
				continue
			}
			var labels []string
			for index, cell := range row {
				if _, isModel := columns[index]; !isModel {
					labels = append(labels, cell)
				}
			}
			unitLabel := normalizeCNOfficialText(strings.Join(labels, " "))
			if !strings.Contains(unitLabel, "1minputtokens") && !strings.Contains(unitLabel, "1moutputtokens") {
				return nil, fmt.Errorf("DeepSeek published price unit changed from per million tokens")
			}
			label := []string{"1M Input Tokens (Cache Hit)", "1M Input Tokens (Cache Miss)", "1M Output Tokens"}[metric]
			period := "Off-Peak"
			if peak {
				period = "Peak"
			}
			next := []string{label, period}
			for _, index := range columnOrder {
				if index >= len(row) {
					return nil, fmt.Errorf("DeepSeek published pricing row is incomplete")
				}
				next = append(next, row[index])
			}
			normalizedRow(next)
		}
		_, _ = normalized.WriteString("</table>")
	}
	models, err := parseCNOfficialDeepSeekPricing([]byte(normalized.String()))
	if err != nil {
		return nil, err
	}
	if flash := models["deepseek-flash"]; flash != nil {
		for _, alias := range []string{"deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
			if strings.Contains(text, alias) && strings.Contains(text, "billed at the Flash price") {
				cloned := *flash
				models[alias] = &cloned
			}
		}
	}
	return models, nil
}

func parseCNOfficialMiniMaxDocument(body []byte) (map[string]*LiteLLMModelPricing, error) {
	document := string(body)
	start := strings.Index(document, "\n## LLM\n")
	if start < 0 {
		return parseCNOfficialMiniMaxPricing(body)
	}
	section := document[start+len("\n## LLM\n"):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	if strings.ContainsAny(section, "¥￥") || strings.Contains(section, "CNY") || strings.Contains(section, "RMB") {
		return nil, fmt.Errorf("MiniMax LLM document does not use the international USD currency")
	}
	type publishedTier struct {
		context  string
		standard []string
		priority []string
	}
	tiers := make(map[string]map[int]*publishedTier)
	priority, recognizedTable := false, false
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "**Priority**") {
			priority = true
		} else if line == "**Standard**" {
			priority = false
		}
		if !strings.HasPrefix(line, "|") {
			recognizedTable = false
			continue
		}
		fields := strings.Split(strings.Trim(line, "|"), "|")
		for index := range fields {
			fields[index] = strings.TrimSpace(fields[index])
		}
		if len(fields) > 0 && fields[0] == "**Model**" {
			recognizedTable = len(fields) == 4 &&
				fields[1] == `**Input** \$ / M tokens` &&
				fields[2] == `**Output** \$ / M tokens` &&
				fields[3] == `**Prompt caching Read** \$ / M tokens`
			if !recognizedTable {
				return nil, fmt.Errorf("MiniMax published LLM pricing columns changed")
			}
			continue
		}
		if !recognizedTable || len(fields) == 0 || strings.HasPrefix(fields[0], ":") {
			continue
		}
		match := cnOfficialMiniMaxPublishedModel.FindStringSubmatch(fields[0])
		if len(match) == 0 || len(fields) != 4 {
			return nil, fmt.Errorf("unsupported MiniMax published LLM row")
		}
		model := strings.ToLower(match[1])
		contextLabel := strings.TrimSpace(strings.ReplaceAll(match[2], `\*`, ""))
		contextLabel = strings.TrimSpace(strings.TrimSuffix(contextLabel, "Permanent 50% off"))
		if contextLabel != "" && contextLabel != "≤ 512k input tokens" && contextLabel != "> 512k input tokens" {
			return nil, fmt.Errorf("MiniMax published context tier changed: %q", contextLabel)
		}
		band, err := miniMaxContextTier(contextLabel)
		if err != nil {
			return nil, err
		}
		values := make([]string, 3)
		for index, field := range fields[1:] {
			amount := cnOfficialMiniMaxPublishedAmount.FindStringSubmatch(field)
			if len(amount) == 0 {
				return nil, fmt.Errorf("invalid MiniMax published current price: %q", field)
			}
			values[index] = strings.ReplaceAll(amount[1], `\`, "")
		}
		if tiers[model] == nil {
			tiers[model] = make(map[int]*publishedTier)
		}
		tier := tiers[model][band]
		if tier == nil {
			tier = &publishedTier{context: contextLabel}
			tiers[model][band] = tier
		}
		if priority {
			if tier.priority != nil {
				return nil, fmt.Errorf("duplicate MiniMax published priority tier")
			}
			tier.priority = values
		} else {
			if tier.standard != nil {
				return nil, fmt.Errorf("duplicate MiniMax published standard tier")
			}
			tier.standard = values
		}
	}
	var normalized strings.Builder
	_, _ = normalized.WriteString("USD per 1M tokens\n\n")
	models := make([]string, 0, len(tiers))
	for model := range tiers {
		models = append(models, model)
	}
	sort.Strings(models)
	for _, model := range models {
		base := tiers[model][0]
		if base == nil || base.standard == nil {
			return nil, fmt.Errorf("MiniMax published model has no standard base price")
		}
		header := "| Model | Context | Input USD | Output USD | Cache Read USD |"
		separator := "| --- | --- | --- | --- | --- |"
		withPriority := base.priority != nil
		if withPriority {
			header += " Priority Input USD | Priority Output USD | Priority Cache Read USD |"
			separator += " --- | --- | --- |"
		}
		_, _ = normalized.WriteString(header + "\n" + separator + "\n")
		for _, band := range []int{0, 1} {
			tier := tiers[model][band]
			if tier == nil {
				continue
			}
			if tier.standard == nil || (tier.priority != nil) != withPriority {
				return nil, fmt.Errorf("MiniMax published standard/priority tiers are incomplete")
			}
			fields := append([]string{model, tier.context}, tier.standard...)
			if withPriority {
				fields = append(fields, tier.priority...)
			}
			_, _ = normalized.WriteString("| " + strings.Join(fields, " | ") + " |\n")
		}
		_, _ = normalized.WriteString("\n")
	}
	return parseCNOfficialMiniMaxPricing([]byte(normalized.String()))
}
