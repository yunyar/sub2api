package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const cnOfficialZAIFixture = `All prices are in USD.
## Models
### Latest Models
Prices per 1M tokens.
| Model | Input | Cached Input | Cached Input Storage | Output |
| :- | :- | :- | :- | :- |
| GLM-5.3 | \$1.4 | \$0.26 | Limited-time Free | \$4.4 |
| GLM-5.3-Flash | \$0.15 | \$0.03 | Limited-time Free | \$0.50 |
### Text Models
Prices per 1M tokens.
| Model | Input | Cached Input | Cached Input Storage | Output |
| :- | :- | :- | :- | :- |
| GLM-5.2 | \$1.4 | \$0.26 | Limited-time Free | \$4.4 |
### Image Models
Prices per image.
| Model | Price |
| :- | :- |
| GLM-Image | \$0.015 |
`

func TestCNOfficialZAIParsesUSDTokenTablesOnly(t *testing.T) {
	models, err := parseCNOfficialZAIPricing([]byte(cnOfficialZAIFixture))
	require.NoError(t, err)
	require.Len(t, models, 3)
	require.InDelta(t, 1.4e-6, models["glm-5.3"].InputCostPerToken, 1e-14)
	require.InDelta(t, .26e-6, models["glm-5.3"].CacheReadInputTokenCost, 1e-14)
	require.InDelta(t, 4.4e-6, models["glm-5.3"].OutputCostPerToken, 1e-14)
	require.NotContains(t, models, "glm-image")
}

func TestCNOfficialZAIRejectsChangedUnitsColumnsAndAmounts(t *testing.T) {
	for _, invalid := range []string{
		strings.ReplaceAll(cnOfficialZAIFixture, "USD", "CNY"),
		strings.ReplaceAll(cnOfficialZAIFixture, "1M tokens", "1K tokens"),
		strings.ReplaceAll(cnOfficialZAIFixture, "Cached Input Storage | Output", "Output | Cached Input Storage"),
		strings.ReplaceAll(cnOfficialZAIFixture, `\$1.4`, "Free"),
		strings.ReplaceAll(cnOfficialZAIFixture, `\$1.4`, `\$NaN`),
		strings.ReplaceAll(cnOfficialZAIFixture, "Limited-time Free", `\$0.05/hour`),
		strings.ReplaceAll(cnOfficialZAIFixture, "GLM-5.3 |", "GLM-5.3* |"),
	} {
		_, err := parseCNOfficialZAIPricing([]byte(invalid))
		require.Error(t, err)
	}
}
