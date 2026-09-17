// Package billing provides provider pricing data and cost estimation utilities.
package billing

// PriceTable maps "provider/model" to [inputPer1M, outputPer1M] in USD.
var PriceTable = map[string][2]float64{
	// Anthropic
	"anthropic/claude-opus-4":     {15.00, 75.00},
	"anthropic/claude-sonnet-4":   {3.00, 15.00},
	"anthropic/claude-haiku-4":    {0.80, 4.00},
	"anthropic/claude-opus-3":     {15.00, 75.00},
	"anthropic/claude-sonnet-3-7": {3.00, 15.00},
	"anthropic/claude-sonnet-3-5": {3.00, 15.00},
	"anthropic/claude-haiku-3-5":  {0.80, 4.00},

	// OpenAI
	"openai/gpt-4o":       {2.50, 10.00},
	"openai/gpt-4o-mini":  {0.15, 0.60},
	"openai/gpt-4-turbo":  {10.00, 30.00},
	"openai/gpt-4":        {30.00, 60.00},
	"openai/gpt-3.5-turbo": {0.50, 1.50},
	"openai/o3":           {10.00, 40.00},
	"openai/o3-mini":      {1.10, 4.40},
	"openai/o4-mini":      {1.10, 4.40},

	// Google
	"google/gemini-2.5-pro":   {1.25, 10.00},
	"google/gemini-2.5-flash": {0.075, 0.30},
	"google/gemini-1.5-pro":   {1.25, 5.00},
	"google/gemini-1.5-flash": {0.075, 0.30},

	// Mistral
	"mistral/mistral-large":  {3.00, 9.00},
	"mistral/mistral-medium": {2.70, 8.10},
	"mistral/mistral-small":  {0.10, 0.30},

	// xAI
	"xai/grok-3":      {3.00, 15.00},
	"xai/grok-3-mini": {0.30, 0.50},

	// Meta (via openai-compat providers)
	"meta/llama-3.3-70b":  {0.59, 0.79},
	"meta/llama-3.1-405b": {2.70, 2.70},

	// DeepSeek
	"deepseek/deepseek-chat":   {0.27, 1.10},
	"deepseek/deepseek-reason": {0.55, 2.19},
}

// Estimate returns the cost in USD for a given number of prompt and completion tokens.
// provider and model are looked up as "provider/model"; falls back to zero if not found.
func Estimate(provider, model string, promptTokens, completionTokens int) float64 {
	key := provider + "/" + model
	prices, ok := PriceTable[key]
	if !ok {
		return 0
	}
	promptCost := prices[0] * float64(promptTokens) / 1_000_000
	completionCost := prices[1] * float64(completionTokens) / 1_000_000
	return promptCost + completionCost
}