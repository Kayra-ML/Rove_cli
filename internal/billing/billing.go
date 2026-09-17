package billing

// PriceTable holds per-model pricing in USD per 1 million tokens.
// Keys are "<provider>/<model>".
var PriceTable = map[string][2]float64{
	// {prompt $/1M, completion $/1M}
	"openai/gpt-4o":            {5.00, 15.00},
	"openai/gpt-4o-mini":       {0.15, 0.60},
	"openai/gpt-4-turbo":       {10.00, 30.00},
	"openai/gpt-3.5-turbo":     {0.50, 1.50},
	"anthropic/claude-3-5-sonnet-20241022": {3.00, 15.00},
	"anthropic/claude-3-5-haiku-20241022":  {0.80, 4.00},
	"anthropic/claude-3-opus-20240229":     {15.00, 75.00},
	"anthropic/claude-3-sonnet-20240229":   {3.00, 15.00},
	"anthropic/claude-3-haiku-20240307":    {0.25, 1.25},
	"google/gemini-1.5-pro":    {3.50, 10.50},
	"google/gemini-1.5-flash":  {0.075, 0.30},
	"mistral/mistral-large":    {4.00, 12.00},
	"mistral/mistral-small":    {1.00, 3.00},
	"fake/fake":                {0.00, 0.00},
}

// Estimate returns the cost in USD for a given provider, model and token counts.
// Falls back to zero if the model is unknown.
func Estimate(provider, model string, promptTokens, completionTokens int) float64 {
	key := provider + "/" + model
	prices, ok := PriceTable[key]
	if !ok {
		// Try model-only fallback (e.g. when provider == model name prefix)
		for k, v := range PriceTable {
			if k == model {
				prices = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return 0
	}
	const perMillion = 1_000_000.0
	return (float64(promptTokens)/perMillion)*prices[0] +
		(float64(completionTokens)/perMillion)*prices[1]
}