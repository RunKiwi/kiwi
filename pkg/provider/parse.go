package provider

import (
	"encoding/json"
	"strings"
)

// Pricing defines input and output token rates per million tokens.
//
// Cache rates are optional. A tool-using conversation re-sends its whole
// transcript on every turn, so cached input is not a minor discount — it is
// most of the bill, and pricing it as ordinary input overstates a cached run by
// roughly an order of magnitude. That matters beyond the invoice: the per-job
// budget cap in LeaseNextTask and the Spend page both read these numbers, so an
// overstatement fails cheap jobs.
//
// Zero means "derive it" (see cacheRates): every provider that offers caching
// prices it as a ratio of the input rate, and the ratios are stable per
// provider. Deriving keeps a model's entry honest by default rather than
// silently billing cache reads at the full input rate — which is what a
// two-field Pricing struct did.
type Pricing struct {
	InputCostPerM  float64
	OutputCostPerM float64
	// CacheReadPerM is the rate for tokens served from cache. Zero derives it.
	CacheReadPerM float64
	// CacheWritePerM is the rate for tokens written to cache. Zero derives it.
	CacheWritePerM float64
}

// PricingMap stores token pricing for common models.
var PricingMap = map[string]Pricing{
	"claude-opus-4-8":   {InputCostPerM: 5.00, OutputCostPerM: 25.00},
	"claude-sonnet-5":   {InputCostPerM: 3.00, OutputCostPerM: 15.00},
	"claude-3-5-sonnet": {InputCostPerM: 3.00, OutputCostPerM: 15.00},
	"claude-3-5-haiku":  {InputCostPerM: 0.80, OutputCostPerM: 4.00},
	// The default worker model. Without an entry it fell back to Opus pricing —
	// five times its real input rate and five times its output rate — so the
	// per-job budget cap tripped early and the Spend page overstated every job
	// run on the default.
	"claude-haiku-4-5-20251001": {InputCostPerM: 1.00, OutputCostPerM: 5.00},
	"gemini-2.0-flash":          {InputCostPerM: 0.10, OutputCostPerM: 0.40},
	// The alias the dashboard offers. Without an entry it fell back to
	// gemini-2.0-flash pricing, which is close but leaves the per-job budget cap
	// and the Spend page quietly approximating a model most tasks actually use.
	"gemini-flash-latest": {InputCostPerM: 0.30, OutputCostPerM: 2.50},
	"gemini-1.5-flash":    {InputCostPerM: 0.075, OutputCostPerM: 0.30},
	"gemini-1.5-pro":      {InputCostPerM: 1.25, OutputCostPerM: 5.00},
	"gpt-5":               {InputCostPerM: 1.25, OutputCostPerM: 10.00},
	"gpt-5-mini":          {InputCostPerM: 0.25, OutputCostPerM: 2.00},
	"gpt-5-nano":          {InputCostPerM: 0.05, OutputCostPerM: 0.40},
	"gpt-4.1":             {InputCostPerM: 2.00, OutputCostPerM: 8.00},
	"gpt-4.1-mini":        {InputCostPerM: 0.40, OutputCostPerM: 1.60},
	"gpt-4o":              {InputCostPerM: 2.50, OutputCostPerM: 10.00},
	"gpt-4o-mini":         {InputCostPerM: 0.15, OutputCostPerM: 0.60},
}

// ModelCostUSD computes the cost of a call given token usage and model pricing.
func ModelCostUSD(model string, inputTokens, outputTokens int64) float64 {
	// Clean model prefix if any (e.g. from SDK types)
	cleaned := strings.TrimPrefix(model, "Model")
	p, ok := PricingMap[cleaned]
	if !ok {
		// Fall back to a same-family default so an unlisted model isn't billed at
		// the wrong provider's (much higher) rate. The family comes from
		// ProviderOf, so pricing and key routing can never disagree about which
		// provider a model belongs to.
		switch ProviderOf(cleaned) {
		case ProviderGemini:
			p = PricingMap["gemini-2.0-flash"]
		case ProviderOpenAI:
			p = PricingMap["gpt-5-mini"]
		default:
			p = PricingMap["claude-opus-4-8"]
		}
	}
	return float64(inputTokens)/1e6*p.InputCostPerM + float64(outputTokens)/1e6*p.OutputCostPerM
}

// pricingFor resolves a model's pricing with the same family fallback
// ModelCostUSD uses, so the two can never disagree about what a model costs.
func pricingFor(model string) Pricing {
	cleaned := strings.TrimPrefix(model, "Model")
	if p, ok := PricingMap[cleaned]; ok {
		return p
	}
	switch ProviderOf(cleaned) {
	case ProviderGemini:
		return PricingMap["gemini-2.0-flash"]
	case ProviderOpenAI:
		return PricingMap["gpt-5-mini"]
	default:
		return PricingMap["claude-opus-4-8"]
	}
}

// Cache rate multipliers, applied to a model's input rate when its Pricing
// entry does not state them outright.
//
// These are per-provider conventions, not per-model ones, which is why
// deriving is safe: Anthropic charges a quarter extra to write a 5-minute entry
// and a tenth to read one; Gemini and OpenAI do not bill a separate write at
// all and discount reads. A model whose provider changes its terms gets an
// explicit entry in PricingMap and stops deriving — that is what the fields are
// for.
//
// Where a guess is unavoidable it is made expensive rather than cheap. An
// under-estimate spends a customer's money past their cap; an over-estimate
// stops early and says so.
const (
	anthropicCacheReadRatio  = 0.10
	anthropicCacheWriteRatio = 1.25
	geminiCacheReadRatio     = 0.25
	openaiCacheReadRatio     = 0.10
)

// cacheRates returns the per-million cache read/write rates for a model.
func cacheRates(model string) (read, write float64) {
	p := pricingFor(model)
	read, write = p.CacheReadPerM, p.CacheWritePerM
	if read > 0 && write > 0 {
		return read, write
	}

	var readRatio, writeRatio float64
	switch ProviderOf(strings.TrimPrefix(model, "Model")) {
	case ProviderGemini:
		readRatio, writeRatio = geminiCacheReadRatio, 1.0
	case ProviderOpenAI:
		readRatio, writeRatio = openaiCacheReadRatio, 1.0
	default:
		readRatio, writeRatio = anthropicCacheReadRatio, anthropicCacheWriteRatio
	}
	if read <= 0 {
		read = p.InputCostPerM * readRatio
	}
	if write <= 0 {
		write = p.InputCostPerM * writeRatio
	}
	return read, write
}

// ModelCostUSDWithCache prices a call that used prompt caching.
//
// inputTokens must be the tokens billed at the full input rate — the cache
// classes are reported separately by every provider that offers them, and
// double-counting them here would defeat the point. ModelCostUSD remains the
// right call for a single-turn request that used no cache.
func ModelCostUSDWithCache(model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64) float64 {
	p := pricingFor(model)
	readRate, writeRate := cacheRates(model)
	return float64(inputTokens)/1e6*p.InputCostPerM +
		float64(outputTokens)/1e6*p.OutputCostPerM +
		float64(cacheReadTokens)/1e6*readRate +
		float64(cacheWriteTokens)/1e6*writeRate
}

// CacheDiscountUSD returns the dollar savings achieved by serving cachedTokens
// from prompt cache at the cache-read rate rather than the standard input rate
// for the given model.
func CacheDiscountUSD(model string, cachedTokens int64) float64 {
	if cachedTokens <= 0 {
		return 0
	}
	p := pricingFor(model)
	readRate, _ := cacheRates(model)
	if p.InputCostPerM <= readRate {
		return 0
	}
	return float64(cachedTokens) / 1e6 * (p.InputCostPerM - readRate)
}

// costUSD computes the cost of a call given token usage at default Opus 4.8 pricing.
func costUSD(inputTokens, outputTokens int64) float64 {
	return ModelCostUSD("claude-opus-4-8", inputTokens, outputTokens)
}

// extractCode returns the contents of the first fenced code block in s.
// If there is no fence, it returns the whole string trimmed.
func extractCode(s string) string {
	start := strings.Index(s, "```")
	if start == -1 {
		return strings.TrimSpace(s)
	}
	rest := s[start+3:]
	// Drop the remainder of the fence line (optional language tag).
	if nl := strings.IndexByte(rest, '\n'); nl != -1 {
		rest = rest[nl+1:]
	}
	if end := strings.Index(rest, "```"); end != -1 {
		return strings.TrimSpace(rest[:end])
	}
	return strings.TrimSpace(rest)
}

// parseVerdict extracts the first JSON object from s and unmarshals it into a
// Verdict. Any failure is treated as a rejection (fail safe).
func parseVerdict(s string) Verdict {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end < start {
		return Verdict{Approved: false, Reasons: "could not parse critic verdict: no JSON object found"}
	}
	var v Verdict
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return Verdict{Approved: false, Reasons: "could not parse critic verdict: " + err.Error()}
	}
	return v
}
