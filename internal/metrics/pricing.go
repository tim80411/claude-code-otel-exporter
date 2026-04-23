package metrics

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
)

//go:embed pricing.json
var pricingJSON []byte

// ModelPricing holds per-million-token prices in USD.
type ModelPricing struct {
	InputPerMTok         float64 `json:"input_per_mtok"`
	OutputPerMTok        float64 `json:"output_per_mtok"`
	CacheReadPerMTok     float64 `json:"cache_read_per_mtok"`
	CacheCreationPerMTok float64 `json:"cache_creation_per_mtok"`
	// LiteLLMKey maps this entry to the LiteLLM model_prices_and_context_window.json
	// key used by scripts/pricing-sync.sh. Unused at runtime.
	LiteLLMKey string `json:"litellm_key,omitempty"`
}

type pricingFile struct {
	LastUpdated string                  `json:"last_updated"`
	Models      map[string]ModelPricing `json:"models"`
	Aliases     map[string]string       `json:"aliases"`
}

// dateSuffixRE matches a trailing `-YYYYMMDD` that Claude Code may append to
// model names (e.g. `claude-haiku-4-5-20251001`).
var dateSuffixRE = regexp.MustCompile(`-\d{8}$`)

var loadedPricing pricingFile

func init() {
	if err := json.Unmarshal(pricingJSON, &loadedPricing); err != nil {
		panic(fmt.Sprintf("metrics: failed to parse embedded pricing.json: %v", err))
	}
	if len(loadedPricing.Models) == 0 {
		panic("metrics: embedded pricing.json has no models")
	}
}

// PricingTable exposes the loaded pricing map for callers that need direct
// access (e.g. tests iterating over known models). New code should prefer
// LookupPricing which understands aliases and date suffixes.
func PricingTable() map[string]ModelPricing {
	return loadedPricing.Models
}

// LookupPricing returns pricing for a model and whether it was found.
// Resolution order:
//  1. alias → canonical name
//  2. exact name
//  3. strip trailing `-YYYYMMDD` date suffix, repeat alias + exact lookup
func LookupPricing(model string) (ModelPricing, bool) {
	if p, ok := resolve(model); ok {
		return p, true
	}
	if stripped := dateSuffixRE.ReplaceAllString(model, ""); stripped != "" && stripped != model {
		if p, ok := resolve(stripped); ok {
			return p, true
		}
	}
	return ModelPricing{}, false
}

func resolve(name string) (ModelPricing, bool) {
	if canonical, ok := loadedPricing.Aliases[name]; ok {
		name = canonical
	}
	p, ok := loadedPricing.Models[name]
	return p, ok
}

// ComputeCost calculates USD cost from token counts and pricing.
func ComputeCost(input, output, cacheRead, cacheCreation int, p ModelPricing) float64 {
	return float64(input)*p.InputPerMTok/1_000_000 +
		float64(output)*p.OutputPerMTok/1_000_000 +
		float64(cacheRead)*p.CacheReadPerMTok/1_000_000 +
		float64(cacheCreation)*p.CacheCreationPerMTok/1_000_000
}
