package metrics

import "testing"

func TestLookupPricing(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantFound bool
	}{
		{"exact opus 4.7", "claude-opus-4-7", true},
		{"exact opus 4.6", "claude-opus-4-6", true},
		{"exact sonnet 4.6", "claude-sonnet-4-6", true},
		{"exact haiku 4.5", "claude-haiku-4-5", true},
		{"haiku 4.5 with date suffix", "claude-haiku-4-5-20251001", true},
		{"opus 4.7 with date suffix", "claude-opus-4-7-20260416", true},
		{"alias haiku", "haiku", true},
		{"alias sonnet", "sonnet", true},
		{"alias opus", "opus", true},
		{"unknown model", "gpt-4", false},
		{"synthetic model", "<synthetic>", false},
		{"empty model", "", false},
		{"suffix-only no base", "-20251001", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := LookupPricing(tt.model)
			if ok != tt.wantFound {
				t.Errorf("LookupPricing(%q) found=%v, want %v", tt.model, ok, tt.wantFound)
			}
		})
	}
}

func TestLookupPricing_DateSuffixMatchesBase(t *testing.T) {
	base, okBase := LookupPricing("claude-haiku-4-5")
	stripped, okStripped := LookupPricing("claude-haiku-4-5-20251001")
	if !okBase || !okStripped {
		t.Fatal("expected both lookups to succeed")
	}
	if base != stripped {
		t.Errorf("date-suffixed lookup should return same pricing as base: base=%+v stripped=%+v", base, stripped)
	}
}

func TestLookupPricing_AliasMatchesCanonical(t *testing.T) {
	cases := []struct{ alias, canonical string }{
		{"opus", "claude-opus-4-7"},
		{"sonnet", "claude-sonnet-4-6"},
		{"haiku", "claude-haiku-4-5"},
	}
	for _, c := range cases {
		t.Run(c.alias, func(t *testing.T) {
			aliased, ok1 := LookupPricing(c.alias)
			canonical, ok2 := LookupPricing(c.canonical)
			if !ok1 || !ok2 {
				t.Fatalf("expected both %q and %q to be found", c.alias, c.canonical)
			}
			// LiteLLMKey is metadata; ignore it for pricing equality.
			aliased.LiteLLMKey = ""
			canonical.LiteLLMKey = ""
			if aliased != canonical {
				t.Errorf("alias %q and canonical %q differ: %+v vs %+v", c.alias, c.canonical, aliased, canonical)
			}
		})
	}
}

// TestPricingValues pins the prices so accidental edits to pricing.json
// become visible failures. Update when intentionally syncing prices.
func TestPricingValues(t *testing.T) {
	want := map[string]ModelPricing{
		"claude-opus-4-7":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.5, CacheCreationPerMTok: 6.25},
		"claude-opus-4-6":   {InputPerMTok: 5.0, OutputPerMTok: 25.0, CacheReadPerMTok: 0.5, CacheCreationPerMTok: 6.25},
		"claude-sonnet-4-6": {InputPerMTok: 3.0, OutputPerMTok: 15.0, CacheReadPerMTok: 0.3, CacheCreationPerMTok: 3.75},
		"claude-haiku-4-5":  {InputPerMTok: 1.0, OutputPerMTok: 5.0, CacheReadPerMTok: 0.1, CacheCreationPerMTok: 1.25},
	}
	for model, expected := range want {
		got, ok := LookupPricing(model)
		if !ok {
			t.Errorf("%s: not found", model)
			continue
		}
		got.LiteLLMKey = "" // ignore metadata
		if got != expected {
			t.Errorf("%s: got %+v, want %+v", model, got, expected)
		}
	}
}
