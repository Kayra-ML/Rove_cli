// Package billing provides provider pricing data and cost estimation utilities.
package billing

// PriceTable maps "provider/model" to [inputPer1M, outputPer1M] in USD.
var PriceTable = map[string][2]float64{
	// Anthropic
	"anthropic/claude-opus-4":              {15.00, 75.00},
	"anthropic/claude-sonnet-4-6":          {3.00, 15.00},
	"anthropic/claude-haiku-4-5":           {0.25, 1.25},
	"anthropic/claude-3-5-sonnet-20241022": {3.00, 15.00},
	"anthropic/claude-3-5-haiku-20241022":  {0.80, 4.00},
	"anthropic/claude-3-opus-20240229":     {15.00, 75.00},
	// OpenAI
	"openai/gpt-4o":       {5.00, 15.00},
	"openai/gpt-4o-mini":  {0.15, 0.60},
	"openai/o3":           {10.00, 40.00},
	"openai/o4-mini":      {1.10, 4.40},
	// Google
	"google/gemini-2.5-pro":   {3.50, 10.50},
	"google/gemini-2.5-flash": {0.30, 2.50},
	// xAI
	"xai/grok-4":     {3.00, 15.00},
	"xai/grok-3":     {3.00, 15.00},
	"xai/grok-3-mini": {0.30, 0.50},
	// DeepSeek
	"deepseek/deepseek-r1":   {0.55, 2.19},
	"deepseek/deepseek-v3":   {0.27, 1.10},
	// Meta / open-source
	"meta/llama-3.3-70b-instruct": {0.23, 0.40},
}

// Estimate returns the estimated cost in USD for a given provider, model, prompt tokens, and completion tokens.
// Falls back to a default rate if the model is not in the price table.
func Estimate(provider, model string, promptTokens, completionTokens int) float64 {
	key := provider + "/" + model
	prices, ok := PriceTable[key]
	if !ok {
		// Default: $1 / 1M input, $3 / 1M output
		prices = [2]float64{1.00, 3.00}
	}
	inputCost := prices[0] * float64(promptTokens) / 1_000_000
	outputCost := prices[1] * float64(completionTokens) / 1_000_000
	return inputCost + outputCost
}

// AllPrices returns the full price table as a slice of PriceEntry for serialization.
func AllPrices() []PriceEntry {
	out := make([]PriceEntry, 0, len(PriceTable))
	for key, prices := range PriceTable {
		out = append(out, PriceEntry{
			Key:         key,
			InputPer1M:  prices[0],
			OutputPer1M: prices[1],
		})
	}
	return out
}

// PriceEntry is a single row in the price table.
type PriceEntry struct {
	Key         string  `json:"key"`
	InputPer1M  float64 `json:"inputPer1M"`
	OutputPer1M float64 `json:"outputPer1M"`
}