package provider

import (
	"encoding/json"
	"strings"
)

// Pricing defines input and output token rates per million tokens.
type Pricing struct {
	InputCostPerM  float64
	OutputCostPerM float64
}

// PricingMap stores token pricing for common models.
var PricingMap = map[string]Pricing{
	"claude-opus-4-8":   {InputCostPerM: 5.00, OutputCostPerM: 25.00},
	"claude-3-5-sonnet": {InputCostPerM: 3.00, OutputCostPerM: 15.00},
	"claude-3-5-haiku":  {InputCostPerM: 0.80, OutputCostPerM: 4.00},
	"gemini-2.0-flash":  {InputCostPerM: 0.10, OutputCostPerM: 0.40},
	"gemini-1.5-flash":  {InputCostPerM: 0.075, OutputCostPerM: 0.30},
	"gemini-1.5-pro":    {InputCostPerM: 1.25, OutputCostPerM: 5.00},
}

// ModelCostUSD computes the cost of a call given token usage and model pricing.
func ModelCostUSD(model string, inputTokens, outputTokens int64) float64 {
	// Clean model prefix if any (e.g. from SDK types)
	cleaned := strings.TrimPrefix(model, "Model")
	p, ok := PricingMap[cleaned]
	if !ok {
		// Fall back to a same-family default so an unlisted model isn't billed at
		// the wrong provider's (much higher) rate.
		if strings.HasPrefix(cleaned, "gemini") {
			p = PricingMap["gemini-2.0-flash"]
		} else {
			p = PricingMap["claude-opus-4-8"]
		}
	}
	return float64(inputTokens)/1e6*p.InputCostPerM + float64(outputTokens)/1e6*p.OutputCostPerM
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
