package metrics

// ModelPricing holds per-million-token prices in USD.
type ModelPricing struct {
	InputPerMTok         float64
	OutputPerMTok        float64
	CacheReadPerMTok     float64
	CacheCreationPerMTok float64
}

// PricingTable maps model names to their pricing.
var PricingTable = map[string]ModelPricing{
	"claude-opus-4-6": {
		InputPerMTok:         15.0,
		OutputPerMTok:        75.0,
		CacheReadPerMTok:     1.5,
		CacheCreationPerMTok: 18.75,
	},
	"claude-sonnet-4-6": {
		InputPerMTok:         3.0,
		OutputPerMTok:        15.0,
		CacheReadPerMTok:     0.30,
		CacheCreationPerMTok: 3.75,
	},
	"claude-haiku-4-5": {
		InputPerMTok:         0.80,
		OutputPerMTok:        4.0,
		CacheReadPerMTok:     0.08,
		CacheCreationPerMTok: 1.0,
	},
}

// LookupPricing returns pricing for a model and whether it was found.
func LookupPricing(model string) (ModelPricing, bool) {
	p, ok := PricingTable[model]
	return p, ok
}

// ComputeCost calculates USD cost from token counts and pricing.
func ComputeCost(input, output, cacheRead, cacheCreation int, p ModelPricing) float64 {
	return float64(input)*p.InputPerMTok/1_000_000 +
		float64(output)*p.OutputPerMTok/1_000_000 +
		float64(cacheRead)*p.CacheReadPerMTok/1_000_000 +
		float64(cacheCreation)*p.CacheCreationPerMTok/1_000_000
}
