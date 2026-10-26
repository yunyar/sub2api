package service

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const cnOfficialMiniMaxPricingSourceURL = "https://platform.minimax.io/docs/guides/pricing-paygo.md"

var (
	cnOfficialMiniMaxModelID = regexp.MustCompile(`(?i)^minimax-[a-z0-9][a-z0-9._-]*$`)
	cnOfficialUSDPrice       = regexp.MustCompile(`(?i)^(?:(?:usd)\s*)?(?:us)?\$\s*([0-9]+(?:\.[0-9]+)?)(?:\s*(?:/|per)\s*(?:1\s*)?(?:m|million)\s*tokens?|\s*/\s*mtok)?$`)
	cnOfficialBareUSDPrice   = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
	cnOfficialMillionTokens  = regexp.MustCompile(`(?i)(?:1\s*m|million)\s*(?:input\s*)?tokens?`)
	cnOfficialTableSeparator = regexp.MustCompile(`^:?-{3,}:?$`)
)

type cnOfficialMiniMaxColumns struct {
	model, context int
	input, output  int
	cacheRead      int
	inputPriority  int
	outputPriority int
	cachePriority  int
}

type cnOfficialMiniMaxRates struct {
	input, output, cacheRead float64
	inputPriority            float64
	outputPriority           float64
	cachePriority            float64
	hasCache                 bool
	hasPriority              bool
	hasPriorityCache         bool
}

func parseCNOfficialMiniMaxPricing(body []byte) (map[string]*LiteLLMModelPricing, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, fmt.Errorf("MiniMax official pricing document is empty")
	}
	tables, documentText, err := parseCNOfficialTables(body)
	if err != nil {
		return nil, fmt.Errorf("parse MiniMax official pricing document: %w", err)
	}
	if err := requireCNOfficialUSDPerMillion(documentText); err != nil {
		return nil, fmt.Errorf("MiniMax official pricing: %w", err)
	}

	tiers := make(map[string]map[int]cnOfficialMiniMaxRates)
	for _, table := range tables {
		headerIndex, columns, ok := findCNOfficialMiniMaxHeader(table)
		if !ok {
			continue
		}
		unitText := strings.Join(table[headerIndex], " ")
		if !strings.Contains(strings.ToLower(unitText), "usd") &&
			!strings.Contains(strings.ToLower(documentText), "usd") {
			return nil, fmt.Errorf("MiniMax pricing table does not identify USD units")
		}

		for _, row := range table[headerIndex+1:] {
			if columns.model >= len(row) {
				continue
			}
			model := strings.ToLower(strings.TrimSpace(row[columns.model]))
			if !cnOfficialMiniMaxModelID.MatchString(model) {
				continue
			}

			band, tierErr := miniMaxContextTier(cellAt(row, columns.context))
			if tierErr != nil {
				return nil, fmt.Errorf("MiniMax %s: %w", model, tierErr)
			}
			rates, rateErr := parseMiniMaxRates(row, columns)
			if rateErr != nil {
				return nil, fmt.Errorf("MiniMax %s: %w", model, rateErr)
			}
			modelTiers := tiers[model]
			if modelTiers == nil {
				modelTiers = make(map[int]cnOfficialMiniMaxRates)
				tiers[model] = modelTiers
			}
			if _, duplicate := modelTiers[band]; duplicate {
				return nil, fmt.Errorf("duplicate MiniMax pricing tier for %s", model)
			}
			modelTiers[band] = rates
		}
	}
	if len(tiers) == 0 {
		return nil, fmt.Errorf("MiniMax official pricing document contains no supported model pricing table")
	}

	models := make(map[string]*LiteLLMModelPricing, len(tiers))
	for model, modelTiers := range tiers {
		base, hasBase := modelTiers[0]
		long, hasLong := modelTiers[1]
		if !hasBase {
			return nil, fmt.Errorf("MiniMax %s has a long-context price without its base tier", model)
		}
		pricing := &LiteLLMModelPricing{
			InputCostPerToken:       base.input,
			OutputCostPerToken:      base.output,
			CacheReadInputTokenCost: base.cacheRead,
			SupportsPromptCaching:   base.hasCache || base.hasPriorityCache,
			SupportsServiceTier:     base.hasPriority,
			LiteLLMProvider:         "minimax",
			Mode:                    "chat",
		}
		if base.hasPriority {
			pricing.InputCostPerTokenPriority = base.inputPriority
			pricing.OutputCostPerTokenPriority = base.outputPriority
			pricing.CacheReadInputTokenCostPriority = base.cachePriority
		}
		if hasLong {
			if len(modelTiers) != 2 {
				return nil, fmt.Errorf("MiniMax %s has unsupported additional context tiers; skipping model", model)
			}
			inputMultiplier, err := tierMultiplier(model, "input", base.input, long.input)
			if err != nil {
				return nil, err
			}
			outputMultiplier, err := tierMultiplier(model, "output", base.output, long.output)
			if err != nil {
				return nil, err
			}
			if base.hasCache != long.hasCache {
				return nil, fmt.Errorf("MiniMax %s has inconsistent cache pricing across context tiers; skipping model", model)
			}
			if base.hasCache {
				cacheMultiplier, err := tierMultiplier(model, "cache read", base.cacheRead, long.cacheRead)
				if err != nil {
					return nil, err
				}
				if !closeOfficialPrice(cacheMultiplier, inputMultiplier) {
					return nil, fmt.Errorf("MiniMax %s cache-read tier cannot be represented by the input multiplier; skipping model", model)
				}
			}
			if base.hasPriority != long.hasPriority {
				return nil, fmt.Errorf("MiniMax %s has inconsistent priority pricing across context tiers; skipping model", model)
			}
			if base.hasPriorityCache != long.hasPriorityCache {
				return nil, fmt.Errorf("MiniMax %s has inconsistent priority cache pricing across context tiers; skipping model", model)
			}
			if base.hasPriority {
				priorityInputMultiplier, err := tierMultiplier(model, "priority input", base.inputPriority, long.inputPriority)
				if err != nil {
					return nil, err
				}
				priorityOutputMultiplier, err := tierMultiplier(model, "priority output", base.outputPriority, long.outputPriority)
				if err != nil {
					return nil, err
				}
				if !closeOfficialPrice(priorityInputMultiplier, inputMultiplier) ||
					!closeOfficialPrice(priorityOutputMultiplier, outputMultiplier) {
					return nil, fmt.Errorf("MiniMax %s priority context tiers cannot be represented by the standard multipliers; skipping model", model)
				}
				if base.hasPriorityCache {
					priorityCacheMultiplier, err := tierMultiplier(model, "priority cache read", base.cachePriority, long.cachePriority)
					if err != nil {
						return nil, err
					}
					if !closeOfficialPrice(priorityCacheMultiplier, inputMultiplier) {
						return nil, fmt.Errorf("MiniMax %s priority cache-read tier cannot be represented; skipping model", model)
					}
				}
			}
			pricing.LongContextInputTokenThreshold = 512_000
			pricing.LongContextInputCostMultiplier = inputMultiplier
			pricing.LongContextOutputCostMultiplier = outputMultiplier
		} else if len(modelTiers) > 1 {
			return nil, fmt.Errorf("MiniMax %s has unsupported context tiers; skipping model", model)
		}
		models[model] = pricing
	}
	return models, nil
}

func findCNOfficialMiniMaxHeader(table [][]string) (int, cnOfficialMiniMaxColumns, bool) {
	for rowIndex, row := range table {
		columns := cnOfficialMiniMaxColumns{
			model: -1, context: -1, input: -1, output: -1, cacheRead: -1,
			inputPriority: -1, outputPriority: -1, cachePriority: -1,
		}
		for index, cell := range row {
			label := normalizeCNOfficialText(cell)
			switch {
			case strings.Contains(label, "model") || strings.Contains(label, "型号"):
				columns.model = index
			case strings.Contains(label, "context") || strings.Contains(label, "上下文"):
				columns.context = index
			case (strings.Contains(label, "cache") || strings.Contains(label, "cached")) &&
				(strings.Contains(label, "read") || strings.Contains(label, "hit") || strings.Contains(label, "input")):
				if strings.Contains(label, "priority") {
					columns.cachePriority = index
				} else {
					columns.cacheRead = index
				}
			case strings.Contains(label, "input") || strings.Contains(label, "prompt"):
				if strings.Contains(label, "priority") {
					columns.inputPriority = index
				} else {
					columns.input = index
				}
			case strings.Contains(label, "output") || strings.Contains(label, "completion"):
				if strings.Contains(label, "priority") {
					columns.outputPriority = index
				} else {
					columns.output = index
				}
			}
		}
		if columns.model >= 0 && columns.input >= 0 && columns.output >= 0 {
			return rowIndex, columns, true
		}
	}
	return 0, cnOfficialMiniMaxColumns{}, false
}

func parseMiniMaxRates(row []string, columns cnOfficialMiniMaxColumns) (cnOfficialMiniMaxRates, error) {
	parse := func(column int) (float64, error) {
		value, err := parseCNOfficialUSDPrice(cellAt(row, column), true)
		if err != nil {
			return 0, err
		}
		return value / 1_000_000, nil
	}
	var rates cnOfficialMiniMaxRates
	var err error
	if rates.input, err = parse(columns.input); err != nil {
		return rates, fmt.Errorf("input price: %w", err)
	}
	if rates.output, err = parse(columns.output); err != nil {
		return rates, fmt.Errorf("output price: %w", err)
	}
	if columns.cacheRead >= 0 {
		rates.cacheRead, err = parse(columns.cacheRead)
		if err != nil {
			return rates, fmt.Errorf("cache-read price: %w", err)
		}
		rates.hasCache = true
	}
	if columns.inputPriority >= 0 || columns.outputPriority >= 0 || columns.cachePriority >= 0 {
		if columns.inputPriority < 0 || columns.outputPriority < 0 {
			return rates, fmt.Errorf("incomplete priority input/output columns")
		}
		rates.inputPriority, err = parse(columns.inputPriority)
		if err != nil {
			return rates, fmt.Errorf("priority input price: %w", err)
		}
		rates.outputPriority, err = parse(columns.outputPriority)
		if err != nil {
			return rates, fmt.Errorf("priority output price: %w", err)
		}
		if columns.cachePriority >= 0 {
			rates.cachePriority, err = parse(columns.cachePriority)
			if err != nil {
				return rates, fmt.Errorf("priority cache-read price: %w", err)
			}
			rates.hasPriorityCache = true
		}
		rates.hasPriority = true
	}
	return rates, nil
}

func miniMaxContextTier(raw string) (int, error) {
	label := strings.ToLower(strings.TrimSpace(raw))
	compact := normalizeCNOfficialText(label)
	if strings.Contains(label, ">") || strings.Contains(label, "over ") || strings.Contains(label, "above ") {
		if strings.Contains(compact, "512k") {
			return 1, nil
		}
		return 0, fmt.Errorf("unsupported long-context tier %q; skipping model", raw)
	}
	if strings.Contains(label, "≤") || strings.Contains(label, "<=") || strings.Contains(label, "up to") {
		if strings.Contains(compact, "512k") {
			return 0, nil
		}
		return 0, fmt.Errorf("unsupported base context tier %q; skipping model", raw)
	}
	if strings.Contains(label, "<") || strings.Contains(label, "~") ||
		(strings.Contains(label, "-") && strings.Contains(compact, "512k")) {
		return 0, fmt.Errorf("unsupported context range %q; skipping model", raw)
	}
	return 0, nil
}

func cellAt(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

func parseCNOfficialTables(body []byte) ([][][]string, string, error) {
	source := string(body)
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return nil, "", err
	}
	var tables [][][]string
	var documentParts []string
	var collectText func(*html.Node)
	collectText = func(node *html.Node) {
		if node.Type == html.TextNode {
			if text := strings.TrimSpace(node.Data); text != "" {
				documentParts = append(documentParts, text)
			}
		}
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style") {
			return
		}
		if node.Type == html.ElementNode && node.Data == "table" {
			if rows := readCNOfficialHTMLTable(node); len(rows) > 0 {
				tables = append(tables, rows)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collectText(child)
		}
	}
	collectText(document)
	if len(tables) == 0 {
		tables = parseCNOfficialMarkdownTables(source)
		if len(strings.TrimSpace(source)) > 0 {
			documentParts = []string{source}
		}
	}
	return tables, strings.Join(documentParts, " "), nil
}

type cnOfficialHTMLCell struct {
	text    string
	colSpan int
	rowSpan int
}

type cnOfficialHTMLSpan struct {
	text      string
	remaining int
}

func readCNOfficialHTMLTable(table *html.Node) [][]string {
	var sourceRows [][]cnOfficialHTMLCell
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && child.Data == "table" && child != table {
				continue
			}
			if child.Type == html.ElementNode && child.Data == "tr" {
				var row []cnOfficialHTMLCell
				for cell := child.FirstChild; cell != nil; cell = cell.NextSibling {
					if cell.Type != html.ElementNode || (cell.Data != "td" && cell.Data != "th") {
						continue
					}
					var parts []string
					collectHTMLCellText(cell, &parts)
					row = append(row, cnOfficialHTMLCell{
						text:    strings.Join(parts, " "),
						colSpan: htmlAttrInt(cell, "colspan", 1),
						rowSpan: htmlAttrInt(cell, "rowspan", 1),
					})
				}
				if len(row) > 0 {
					sourceRows = append(sourceRows, row)
				}
				continue
			}
			visit(child)
		}
	}
	visit(table)

	var result [][]string
	active := make(map[int]cnOfficialHTMLSpan)
	for _, sourceRow := range sourceRows {
		row := make(map[int]string)
		nextActive := make(map[int]cnOfficialHTMLSpan)
		for column, span := range active {
			row[column] = span.text
			if span.remaining > 1 {
				span.remaining--
				nextActive[column] = span
			}
		}
		column := 0
		for _, cell := range sourceRow {
			for row[column] != "" {
				column++
			}
			colSpan := max(1, cell.colSpan)
			for offset := 0; offset < colSpan; offset++ {
				row[column+offset] = cell.text
				if cell.rowSpan > 1 {
					nextActive[column+offset] = cnOfficialHTMLSpan{text: cell.text, remaining: cell.rowSpan - 1}
				}
			}
			column += colSpan
		}
		maxColumn := -1
		for position := range row {
			if position > maxColumn {
				maxColumn = position
			}
		}
		expanded := make([]string, maxColumn+1)
		for position, text := range row {
			expanded[position] = text
		}
		result = append(result, expanded)
		active = nextActive
	}
	return result
}

func collectHTMLCellText(node *html.Node, parts *[]string) {
	if node.Type == html.TextNode {
		if text := strings.TrimSpace(node.Data); text != "" {
			*parts = append(*parts, text)
		}
	}
	if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style") {
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		collectHTMLCellText(child, parts)
	}
}

func htmlAttrInt(node *html.Node, name string, fallback int) int {
	for _, attr := range node.Attr {
		if attr.Key == name {
			value, err := strconv.Atoi(attr.Val)
			if err == nil && value > 0 {
				return value
			}
		}
	}
	return fallback
}

func parseCNOfficialMarkdownTables(source string) [][][]string {
	var tables [][][]string
	var current [][]string
	flush := func() {
		if len(current) > 0 {
			tables = append(tables, current)
			current = nil
		}
	}
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			flush()
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for index := range cells {
			cells[index] = strings.TrimSpace(strings.ReplaceAll(cells[index], `\|`, "|"))
		}
		separator := len(cells) > 0
		for _, cell := range cells {
			if !cnOfficialTableSeparator.MatchString(strings.ReplaceAll(cell, " ", "")) {
				separator = false
				break
			}
		}
		if !separator {
			current = append(current, cells)
		}
	}
	flush()
	return tables
}

func normalizeCNOfficialText(raw string) string {
	text := strings.ToLower(raw)
	text = strings.NewReplacer(
		"\u00a0", " ", "\u202f", " ", "\u2011", "-", "\u2013", "-", "\u2014", "-",
		"≤", "<=", "≥", ">=", "\t", " ", "\n", " ",
	).Replace(text)
	text = strings.Join(strings.Fields(text), "")
	text = strings.ReplaceAll(text, "-", "")
	text = strings.ReplaceAll(text, "_", "")
	return text
}

func requireCNOfficialUSDPerMillion(documentText string) error {
	lower := strings.ToLower(documentText)
	if !strings.Contains(lower, "usd") && !strings.Contains(lower, "us dollar") {
		return fmt.Errorf("document does not explicitly identify USD prices")
	}
	if !cnOfficialMillionTokens.MatchString(documentText) &&
		!regexp.MustCompile(`(?i)tokens?\s*(?:/|per)\s*(?:1\s*)?million`).MatchString(documentText) &&
		!regexp.MustCompile(`(?i)(?:/|per)\s*mtok\b`).MatchString(documentText) {
		return fmt.Errorf("document does not identify a per-million-token price unit")
	}
	return nil
}

func parseCNOfficialUSDPrice(raw string, allowBare bool) (float64, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\u00a0", " "))
	match := cnOfficialUSDPrice.FindStringSubmatch(value)
	if len(match) == 0 && allowBare && cnOfficialBareUSDPrice.MatchString(value) {
		match = []string{"", value}
	}
	if len(match) == 0 {
		return 0, fmt.Errorf("price %q is not an explicit USD amount", raw)
	}
	price, err := strconv.ParseFloat(match[1], 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return 0, fmt.Errorf("invalid nonnegative USD price %q", raw)
	}
	return price, nil
}

func tierMultiplier(model, kind string, base, long float64) (float64, error) {
	if base == 0 && long == 0 {
		return 1, nil
	}
	if base <= 0 || long < base || math.IsNaN(base) || math.IsNaN(long) ||
		math.IsInf(base, 0) || math.IsInf(long, 0) {
		return 0, fmt.Errorf("MiniMax %s has unsupported %s long-context pricing; skipping model", model, kind)
	}
	return long / base, nil
}
