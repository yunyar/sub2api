//go:build unit

package service

import (
	"strings"
	"testing"
)

const cnOfficialDeepSeekPublishedExcerpt = `<title>Models &amp; Pricing | DeepSeek API Docs</title>
<table>
<tr><td colspan="2">MODEL</td><td>deepseek-flash<sup>(1)</sup></td><td>deepseek-v4-pro</td></tr>
<tr><td rowspan="2">1M INPUT TOKENS (CACHE HIT)</td><td>OFF-PEAK</td><td>$0.003</td><td>$0.022</td></tr>
<tr><td>PEAK</td><td>$0.006</td><td>$0.044</td></tr>
<tr><td rowspan="2">1M INPUT TOKENS (CACHE MISS)</td><td>OFF-PEAK</td><td>$0.15</td><td>$0.66</td></tr>
<tr><td>PEAK</td><td>$0.3</td><td>$1.32</td></tr>
<tr><td rowspan="2">1M OUTPUT TOKENS</td><td>OFF-PEAK</td><td>$0.6</td><td>$1.98</td></tr>
<tr><td>PEAK</td><td>$1.2</td><td>$3.96</td></tr>
</table>
<p>Peak hours are 01:00 - 04:00 and 06:00 - 10:00 UTC, Monday through Friday, excluding Chinese public holidays.</p>
<p>deepseek-v4-flash and deepseek-v4-flash-vision-exp are billed at the Flash price.</p>`

const cnOfficialMiniMaxPublishedExcerpt = `
## LLM

**Standard**

| **Model** | **Input** \$ / M tokens | **Output** \$ / M tokens | **Prompt caching Read** \$ / M tokens |
| :--- | :--- | :--- | :--- |
| **MiniMax-M3** ≤ 512k input tokens Permanent 50% off | ~~\$0.60~~ \$0.30 | ~~\$2.40~~ \$1.20 | ~~\$0.12~~ \$0.06 |
| **MiniMax-M3** > 512k input tokens Permanent 50% off | ~~\$1.20~~ \$0.60 | ~~\$4.80~~ \$2.40 | ~~\$0.24~~ \$0.12 |

**Priority**

| **Model** | **Input** \$ / M tokens | **Output** \$ / M tokens | **Prompt caching Read** \$ / M tokens |
| :--- | :--- | :--- | :--- |
| **MiniMax-M3** ≤ 512k input tokens | \$0.45 | \$1.80 | \$0.09 |
| **MiniMax-M3** > 512k input tokens | \$0.90 | \$3.60 | \$0.18 |

## Video

| **Model** | Invalid LLM table |
| **MiniMax-Video** | \$100 |
`

func TestCNOfficialDeepSeekPublishedDocument(t *testing.T) {
	models, err := parseCNOfficialDeepSeekDocument([]byte(cnOfficialDeepSeekPublishedExcerpt))
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"deepseek-flash", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp"} {
		assertOfficialPrice(t, models, model, .15e-6, .6e-6, .003e-6)
	}
	assertOfficialPrice(t, models, "deepseek-v4-pro", .66e-6, 1.98e-6, .022e-6)
	for _, unit := range []string{"1K", "1", "1MILLION"} {
		body := strings.ReplaceAll(cnOfficialDeepSeekPublishedExcerpt, "1M ", unit+" ")
		if _, err := parseCNOfficialDeepSeekDocument([]byte(body)); err == nil {
			t.Fatalf("accepted changed price unit %q", unit)
		}
	}
	body := strings.Replace(cnOfficialDeepSeekPublishedExcerpt, "01:00 - 04:00", "00:00 - 04:00", 1)
	if _, err := parseCNOfficialDeepSeekDocument([]byte(body)); err == nil {
		t.Fatal("accepted changed peak schedule")
	}
}

func TestCNOfficialMiniMaxPublishedDocument(t *testing.T) {
	models, err := parseCNOfficialMiniMaxDocument([]byte(cnOfficialMiniMaxPublishedExcerpt))
	if err != nil {
		t.Fatal(err)
	}
	assertOfficialPrice(t, models, "minimax-m3", .30e-6, 1.20e-6, .06e-6)
	if len(models) != 1 {
		t.Fatalf("unexpected media or model prices: %v", models)
	}
	pricing := models["minimax-m3"]
	if pricing.LongContextInputTokenThreshold != 512_000 {
		t.Fatalf("unexpected long-context threshold %d", pricing.LongContextInputTokenThreshold)
	}
	assertClosePrice(t, pricing.LongContextInputCostMultiplier, 2)
	assertClosePrice(t, pricing.InputCostPerTokenPriority, .45e-6)
	assertClosePrice(t, pricing.OutputCostPerTokenPriority, 1.80e-6)
	for _, body := range []string{
		strings.Replace(cnOfficialMiniMaxPublishedExcerpt, "M tokens", "K tokens", 1),
		strings.Replace(cnOfficialMiniMaxPublishedExcerpt, "512k", "1024k", 1),
		strings.Replace(cnOfficialMiniMaxPublishedExcerpt, `\$0.30`, "-", 1),
		strings.Replace(cnOfficialMiniMaxPublishedExcerpt, `\$0.90`, `\$NaN`, 1),
	} {
		if _, err := parseCNOfficialMiniMaxDocument([]byte(body)); err == nil {
			t.Fatal("accepted changed or incomplete published pricing")
		}
	}
}
