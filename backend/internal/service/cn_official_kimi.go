package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var cnOfficialKimiPricePattern = regexp.MustCompile(`^\$\s*((?:[0-9]{1,3}(?:,[0-9]{3})+|[0-9]+)(?:\.[0-9]+)?)\s*/\s*MTok$`)

type cnOfficialKimiColumns struct {
	model    int
	input    int
	output   int
	cache    int
	write5m  int
	write1h  int
	hasWrite bool
}

func parseCNOfficialKimiPricing(body []byte) (map[string]*LiteLLMModelPricing, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("kimi pricing document is empty")
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse Kimi pricing HTML: %w", err)
	}

	native, found, err := parseCNOfficialKimiNativeTables(doc)
	if err != nil {
		return nil, err
	}
	if found {
		return native, nil
	}

	compiled, err := parseCNOfficialKimiCompiledTables(doc)
	if err != nil {
		return nil, err
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("kimi pricing document contains no recognized model-price table")
	}
	return compiled, nil
}

func parseCNOfficialKimiNativeTables(root *html.Node) (map[string]*LiteLLMModelPricing, bool, error) {
	result := make(map[string]*LiteLLMModelPricing)
	found := false
	var visit func(*html.Node) error
	visit = func(node *html.Node) error {
		if node.Type == html.ElementNode && node.Data == "table" {
			tablePricing, candidate, err := parseCNOfficialKimiNativeTable(node)
			if err != nil {
				return err
			}
			if candidate {
				found = true
				if err := mergeCNOfficialKimiModels(result, tablePricing); err != nil {
					return err
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, found, err
	}
	return result, found, nil
}

func parseCNOfficialKimiNativeTable(table *html.Node) (map[string]*LiteLLMModelPricing, bool, error) {
	rows := make([]*html.Node, 0)
	var collect func(*html.Node)
	collect = func(node *html.Node) {
		if node != table && node.Type == html.ElementNode && node.Data == "table" {
			return
		}
		if node.Type == html.ElementNode && node.Data == "tr" {
			rows = append(rows, node)
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			collect(child)
		}
	}
	collect(table)

	headerIndex := -1
	var headers []string
	var headerCells []*html.Node
	for i, row := range rows {
		cells := cnOfficialKimiHTMLCells(row)
		if len(cells) == 0 {
			continue
		}
		allHeaders := true
		for _, cell := range cells {
			if cell.Data != "th" {
				allHeaders = false
				break
			}
		}
		if allHeaders {
			headerIndex = i
			headerCells = cells
			break
		}
	}
	if headerIndex < 0 {
		return nil, false, nil
	}
	headers = make([]string, len(headerCells))
	for i, cell := range headerCells {
		headers[i] = cnOfficialKimiHTMLText(cell)
	}
	columns, candidate, err := cnOfficialKimiColumnIndexes(headers)
	if err != nil || !candidate {
		return nil, candidate, err
	}

	dataRows := make([][]string, 0, len(rows)-headerIndex-1)
	for _, row := range rows[headerIndex+1:] {
		cells := cnOfficialKimiHTMLCells(row)
		if len(cells) == 0 {
			continue
		}
		values := make([]string, len(cells))
		for i, cell := range cells {
			values[i] = cnOfficialKimiHTMLText(cell)
		}
		dataRows = append(dataRows, values)
	}
	result, err := parseCNOfficialKimiRows(headers, columns, dataRows)
	return result, true, err
}

func cnOfficialKimiHTMLCells(row *html.Node) []*html.Node {
	var cells []*html.Node
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && (child.Data == "th" || child.Data == "td") {
			cells = append(cells, child)
		}
	}
	return cells
}

func cnOfficialKimiHTMLText(node *html.Node) string {
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(current *html.Node) {
		if current.Type == html.TextNode {
			_, _ = text.WriteString(current.Data)
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(node)
	return strings.TrimSpace(text.String())
}

func parseCNOfficialKimiCompiledTables(root *html.Node) (map[string]*LiteLLMModelPricing, error) {
	result := make(map[string]*LiteLLMModelPricing)
	found := false
	var visit func(*html.Node) error
	visit = func(node *html.Node) error {
		if node.Type == html.ElementNode && node.Data == "script" {
			for _, source := range cnOfficialKimiRSCStrings(cnOfficialKimiHTMLText(node)) {
				tables, err := cnOfficialKimiDocTables(source)
				if err != nil {
					return err
				}
				for _, table := range tables {
					columns, candidate, err := cnOfficialKimiCompiledColumns(table)
					if err != nil {
						return err
					}
					if !candidate {
						continue
					}
					found = true
					rows, err := cnOfficialKimiCompiledRows(table)
					if err != nil {
						return err
					}
					pricing, err := parseCNOfficialKimiRows(table.columns, columns, rows)
					if err != nil {
						return err
					}
					if err := mergeCNOfficialKimiModels(result, pricing); err != nil {
						return err
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	if found && len(result) == 0 {
		return nil, fmt.Errorf("kimi compiled pricing tables contain no model rows")
	}
	return result, nil
}

type cnOfficialKimiDocTable struct {
	properties map[string]string
	columns    []string
}

func cnOfficialKimiRSCStrings(script string) []string {
	const marker = "self.__next_f.push("
	var decoded []string
	for offset := 0; offset < len(script); {
		found := strings.Index(script[offset:], marker)
		if found < 0 {
			break
		}
		position := offset + found + len(marker)
		skipSpace := func() {
			for position < len(script) && (script[position] == ' ' || script[position] == '\n' || script[position] == '\r' || script[position] == '\t') {
				position++
			}
		}
		skipSpace()
		if position >= len(script) || script[position] != '[' {
			offset = position
			continue
		}
		position++
		skipSpace()
		if position >= len(script) || script[position] != '1' {
			offset = position
			continue
		}
		position++
		skipSpace()
		if position >= len(script) || script[position] != ',' {
			offset = position
			continue
		}
		position++
		skipSpace()
		decoder := json.NewDecoder(strings.NewReader(script[position:]))
		var value string
		if err := decoder.Decode(&value); err != nil {
			offset = position + 1
			continue
		}
		decoded = append(decoded, value)
		offset = position + int(decoder.InputOffset())
	}
	return decoded
}

func cnOfficialKimiDocTables(source string) ([]cnOfficialKimiDocTable, error) {
	var tables []cnOfficialKimiDocTable
	for offset := 0; offset < len(source); {
		position := offset
		if next := strings.Index(source[offset:], "DocTable"); next >= 0 {
			position += next
		} else {
			break
		}
		nameEnd := position + len("DocTable")
		if position > 0 && isCNOfficialKimiJSIdentifier(source[position-1]) ||
			nameEnd < len(source) && isCNOfficialKimiJSIdentifier(source[nameEnd]) {
			offset = nameEnd
			continue
		}
		cursor := nameEnd
		for cursor < len(source) && (source[cursor] == ' ' || source[cursor] == '\n' || source[cursor] == '\r' || source[cursor] == '\t') {
			cursor++
		}
		if cursor >= len(source) || source[cursor] != ',' {
			offset = nameEnd
			continue
		}
		cursor++
		for cursor < len(source) && (source[cursor] == ' ' || source[cursor] == '\n' || source[cursor] == '\r' || source[cursor] == '\t') {
			cursor++
		}
		if cursor >= len(source) || source[cursor] != '{' {
			offset = nameEnd
			continue
		}
		end, err := cnOfficialKimiMatchingJSDelimiter(source, cursor)
		if err != nil {
			return nil, fmt.Errorf("parse Kimi compiled DocTable: %w", err)
		}
		properties, err := cnOfficialKimiJSObjectProperties(source[cursor : end+1])
		if err != nil {
			return nil, fmt.Errorf("parse Kimi compiled DocTable properties: %w", err)
		}
		rawColumns, hasColumns := properties["columns"]
		_, hasRows := properties["rows"]
		if hasColumns && hasRows {
			columnExprs, err := cnOfficialKimiJSArray(rawColumns)
			if err != nil {
				return nil, fmt.Errorf("parse Kimi compiled columns: %w", err)
			}
			columns := make([]string, len(columnExprs))
			for i, expression := range columnExprs {
				columnProps, err := cnOfficialKimiJSObjectProperties(expression)
				if err != nil {
					return nil, fmt.Errorf("parse Kimi compiled column %d: %w", i, err)
				}
				title, ok := columnProps["title"]
				if !ok {
					return nil, fmt.Errorf("kimi compiled column %d has no title", i)
				}
				columns[i], err = cnOfficialKimiJSDisplayText(title)
				if err != nil {
					return nil, fmt.Errorf("parse Kimi compiled column title %d: %w", i, err)
				}
			}
			tables = append(tables, cnOfficialKimiDocTable{properties: properties, columns: columns})
		}
		offset = end + 1
	}
	return tables, nil
}

func cnOfficialKimiCompiledColumns(table cnOfficialKimiDocTable) (cnOfficialKimiColumns, bool, error) {
	columns, candidate, err := cnOfficialKimiColumnIndexes(table.columns)
	return columns, candidate, err
}

func cnOfficialKimiCompiledRows(table cnOfficialKimiDocTable) ([][]string, error) {
	expressions, err := cnOfficialKimiJSArray(table.properties["rows"])
	if err != nil {
		return nil, fmt.Errorf("parse Kimi compiled rows: %w", err)
	}
	rows := make([][]string, len(expressions))
	for rowIndex, expression := range expressions {
		cells, err := cnOfficialKimiJSArray(expression)
		if err != nil {
			return nil, fmt.Errorf("parse Kimi compiled row %d: %w", rowIndex, err)
		}
		rows[rowIndex] = make([]string, len(cells))
		for cellIndex, cell := range cells {
			rows[rowIndex][cellIndex], err = cnOfficialKimiJSDisplayText(cell)
			if err != nil {
				return nil, fmt.Errorf("parse Kimi compiled row %d cell %d: %w", rowIndex, cellIndex, err)
			}
		}
	}
	return rows, nil
}

func cnOfficialKimiColumnIndexes(headers []string) (cnOfficialKimiColumns, bool, error) {
	var result cnOfficialKimiColumns
	for _, index := range []*int{&result.model, &result.input, &result.output, &result.cache, &result.write5m, &result.write1h} {
		*index = -1
	}
	recognized := make([]string, len(headers))
	core := map[string]bool{}
	for i, header := range headers {
		name, ok := cnOfficialKimiColumnName(header)
		if ok {
			recognized[i] = name
			if name == "model" || name == "input" || name == "output" {
				core[name] = true
			}
		}
	}
	candidate := core["model"] && core["input"] && core["output"]
	if !candidate {
		return result, false, nil
	}
	for i, name := range recognized {
		if name == "" {
			return result, true, fmt.Errorf("kimi pricing table has unsupported column %q", headers[i])
		}
		var target *int
		switch name {
		case "model":
			target = &result.model
		case "input":
			target = &result.input
		case "output":
			target = &result.output
		case "cache_read":
			target = &result.cache
		case "write_5m":
			target = &result.write5m
		case "write_1h":
			target = &result.write1h
		}
		if target == nil || *target >= 0 {
			return result, true, fmt.Errorf("kimi pricing table has duplicate or ambiguous column %q", headers[i])
		}
		*target = i
	}
	if result.cache < 0 {
		return result, true, fmt.Errorf("kimi pricing table is missing the cached-input price column")
	}
	if (result.write5m >= 0) != (result.write1h >= 0) {
		return result, true, fmt.Errorf("kimi pricing table must provide both 5m and 1h write prices")
	}
	result.hasWrite = result.write5m >= 0
	return result, true, nil
}

func cnOfficialKimiColumnName(header string) (string, bool) {
	header = strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(header))), " ")
	switch header {
	case "model":
		return "model", true
	case "input":
		return "input", true
	case "output":
		return "output", true
	case "cached input", "cache hit input":
		return "cache_read", true
	case "5m writes":
		return "write_5m", true
	case "1h writes":
		return "write_1h", true
	default:
		return "", false
	}
}

func parseCNOfficialKimiRows(headers []string, columns cnOfficialKimiColumns, rows [][]string) (map[string]*LiteLLMModelPricing, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("kimi pricing table has no model rows")
	}
	result := make(map[string]*LiteLLMModelPricing, len(rows))
	for rowIndex, row := range rows {
		if len(row) != len(headers) {
			return nil, fmt.Errorf("kimi pricing row %d has %d cells for %d columns", rowIndex+1, len(row), len(headers))
		}
		model := strings.TrimSpace(row[columns.model])
		if model == "" || len(strings.Fields(model)) != 1 {
			return nil, fmt.Errorf("kimi pricing row %d has an empty or malformed model name", rowIndex+1)
		}
		if _, exists := result[model]; exists {
			return nil, fmt.Errorf("kimi pricing table contains duplicate model %q", model)
		}

		input, err := cnOfficialKimiTokenPrice(row[columns.input])
		if err != nil {
			return nil, fmt.Errorf("kimi model %q input price: %w", model, err)
		}
		output, err := cnOfficialKimiTokenPrice(row[columns.output])
		if err != nil {
			return nil, fmt.Errorf("kimi model %q output price: %w", model, err)
		}
		cacheRead, err := cnOfficialKimiTokenPrice(row[columns.cache])
		if err != nil {
			return nil, fmt.Errorf("kimi model %q cached-input price: %w", model, err)
		}
		pricing := &LiteLLMModelPricing{
			InputCostPerToken:       input,
			OutputCostPerToken:      output,
			CacheReadInputTokenCost: cacheRead,
			LiteLLMProvider:         "moonshot",
			Mode:                    "chat",
			SupportsPromptCaching:   true,
		}
		if columns.hasWrite {
			write5m, err := cnOfficialKimiTokenPrice(row[columns.write5m])
			if err != nil {
				return nil, fmt.Errorf("kimi model %q 5m cache-write price: %w", model, err)
			}
			write1h, err := cnOfficialKimiTokenPrice(row[columns.write1h])
			if err != nil {
				return nil, fmt.Errorf("kimi model %q 1h cache-write price: %w", model, err)
			}
			pricing.CacheCreationInputTokenCost = write5m
			pricing.CacheCreationInputTokenCostAbove1hr = write1h
			pricing.CacheCreationInputTokenCostExplicit = true
		}
		result[model] = pricing
	}
	return result, nil
}

func cnOfficialKimiTokenPrice(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "free") {
		return 0, fmt.Errorf("free price has no explicit numeric amount")
	}
	match := cnOfficialKimiPricePattern.FindStringSubmatch(value)
	if len(match) != 2 {
		return 0, fmt.Errorf("invalid or missing price %q; expected a USD amount per MTok", value)
	}
	amount, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", ""), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, fmt.Errorf("invalid nonnegative finite price %q", value)
	}
	perToken := amount / 1_000_000
	if math.IsNaN(perToken) || math.IsInf(perToken, 0) || perToken < 0 {
		return 0, fmt.Errorf("invalid nonnegative finite price %q", value)
	}
	return perToken, nil
}

func mergeCNOfficialKimiModels(destination, incoming map[string]*LiteLLMModelPricing) error {
	for model, pricing := range incoming {
		if _, exists := destination[model]; exists {
			return fmt.Errorf("kimi pricing document contains duplicate model %q across tables", model)
		}
		destination[model] = pricing
	}
	return nil
}

func cnOfficialKimiJSObjectProperties(expression string) (map[string]string, error) {
	expression = strings.TrimSpace(expression)
	if len(expression) < 2 || expression[0] != '{' || expression[len(expression)-1] != '}' {
		return nil, fmt.Errorf("expected JavaScript object")
	}
	parts, err := cnOfficialKimiSplitTopLevel(expression[1:len(expression)-1], ',')
	if err != nil {
		return nil, err
	}
	properties := make(map[string]string, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields, err := cnOfficialKimiSplitTopLevel(part, ':')
		if err != nil || len(fields) < 2 {
			return nil, fmt.Errorf("malformed JavaScript object property %q", part)
		}
		key := strings.TrimSpace(fields[0])
		value := strings.TrimSpace(strings.Join(fields[1:], ":"))
		if !isCNOfficialKimiJSIdentifierString(key) || value == "" {
			return nil, fmt.Errorf("malformed JavaScript object property %q", part)
		}
		if _, exists := properties[key]; exists {
			return nil, fmt.Errorf("duplicate JavaScript object property %q", key)
		}
		properties[key] = value
	}
	return properties, nil
}

func cnOfficialKimiJSArray(expression string) ([]string, error) {
	expression = strings.TrimSpace(expression)
	if len(expression) < 2 || expression[0] != '[' || expression[len(expression)-1] != ']' {
		return nil, fmt.Errorf("expected JavaScript array")
	}
	parts, err := cnOfficialKimiSplitTopLevel(expression[1:len(expression)-1], ',')
	if err != nil {
		return nil, err
	}
	var values []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values, nil
}

func cnOfficialKimiJSCallProperties(expression string) (map[string]string, error) {
	expression = strings.TrimSpace(expression)
	open := strings.IndexByte(expression, '(')
	if open < 0 {
		return nil, fmt.Errorf("expected compiled JSX call")
	}
	close, err := cnOfficialKimiMatchingJSDelimiter(expression, open)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(expression[close+1:]) != "" {
		return nil, fmt.Errorf("unexpected content after compiled JSX call")
	}
	args, err := cnOfficialKimiSplitTopLevel(expression[open+1:close], ',')
	if err != nil {
		return nil, err
	}
	var found map[string]string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if len(arg) > 0 && arg[0] == '{' {
			properties, err := cnOfficialKimiJSObjectProperties(arg)
			if err != nil {
				return nil, err
			}
			if _, exists := properties["children"]; exists {
				if found != nil {
					return nil, fmt.Errorf("ambiguous compiled JSX children")
				}
				found = properties
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("compiled JSX value has no direct text child")
	}
	return found, nil
}

func cnOfficialKimiJSDisplayText(expression string) (string, error) {
	if value, ok := cnOfficialKimiJSString(expression); ok {
		return value, nil
	}
	properties, err := cnOfficialKimiJSCallProperties(expression)
	if err != nil {
		return "", err
	}
	value, ok := properties["children"]
	if !ok {
		return "", fmt.Errorf("compiled JSX value has no text child")
	}
	text, ok := cnOfficialKimiJSString(value)
	if !ok {
		return "", fmt.Errorf("compiled JSX text child is not a literal string")
	}
	return text, nil
}

func cnOfficialKimiJSString(expression string) (string, bool) {
	expression = strings.TrimSpace(expression)
	if len(expression) < 2 {
		return "", false
	}
	quote := expression[0]
	if (quote != '`' && quote != '"' && quote != '\'') || expression[len(expression)-1] != quote {
		return "", false
	}
	value := expression[1 : len(expression)-1]
	if quote == '`' && strings.Contains(value, "${") {
		return "", false
	}
	var output strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			if i >= len(value) {
				return "", false
			}
		}
		_ = output.WriteByte(value[i])
	}
	return output.String(), true
}

func cnOfficialKimiSplitTopLevel(expression string, delimiter byte) ([]string, error) {
	var parts []string
	stack := make([]byte, 0, 8)
	start := 0
	for i := 0; i < len(expression); {
		switch expression[i] {
		case '\'', '"', '`':
			next, err := cnOfficialKimiSkipJSString(expression, i)
			if err != nil {
				return nil, err
			}
			i = next
			continue
		case '/':
			if i+1 < len(expression) && expression[i+1] == '/' {
				i += 2
				for i < len(expression) && expression[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(expression) && expression[i+1] == '*' {
				end := strings.Index(expression[i+2:], "*/")
				if end < 0 {
					return nil, fmt.Errorf("unterminated JavaScript comment")
				}
				i += end + 4
				continue
			}
		case '{', '[', '(':
			stack = append(stack, expression[i])
		case '}', ']', ')':
			if len(stack) == 0 || !cnOfficialKimiDelimitersMatch(stack[len(stack)-1], expression[i]) {
				return nil, fmt.Errorf("unbalanced JavaScript delimiters")
			}
			stack = stack[:len(stack)-1]
		default:
			if expression[i] == delimiter && len(stack) == 0 {
				parts = append(parts, expression[start:i])
				start = i + 1
			}
		}
		i++
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed JavaScript delimiter")
	}
	return append(parts, expression[start:]), nil
}

func cnOfficialKimiMatchingJSDelimiter(expression string, open int) (int, error) {
	if open >= len(expression) {
		return 0, fmt.Errorf("missing JavaScript delimiter")
	}
	stack := []byte{expression[open]}
	for i := open + 1; i < len(expression); {
		switch expression[i] {
		case '\'', '"', '`':
			next, err := cnOfficialKimiSkipJSString(expression, i)
			if err != nil {
				return 0, err
			}
			i = next
			continue
		case '/':
			if i+1 < len(expression) && expression[i+1] == '/' {
				i += 2
				for i < len(expression) && expression[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(expression) && expression[i+1] == '*' {
				end := strings.Index(expression[i+2:], "*/")
				if end < 0 {
					return 0, fmt.Errorf("unterminated JavaScript comment")
				}
				i += end + 4
				continue
			}
		case '{', '[', '(':
			stack = append(stack, expression[i])
		case '}', ']', ')':
			if len(stack) == 0 || !cnOfficialKimiDelimitersMatch(stack[len(stack)-1], expression[i]) {
				return 0, fmt.Errorf("unbalanced JavaScript delimiters")
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("unterminated JavaScript expression")
}

func cnOfficialKimiSkipJSString(expression string, start int) (int, error) {
	quote := expression[start]
	for i := start + 1; i < len(expression); i++ {
		if expression[i] == '\\' {
			i++
			continue
		}
		if expression[i] == quote {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated JavaScript string")
}

func cnOfficialKimiDelimitersMatch(open, close byte) bool {
	return open == '{' && close == '}' || open == '[' && close == ']' || open == '(' && close == ')'
}

func isCNOfficialKimiJSIdentifier(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func isCNOfficialKimiJSIdentifierString(value string) bool {
	if value == "" || !isCNOfficialKimiJSIdentifier(value[0]) || value[0] >= '0' && value[0] <= '9' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isCNOfficialKimiJSIdentifier(value[i]) {
			return false
		}
	}
	return true
}
