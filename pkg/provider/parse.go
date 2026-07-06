package provider

import (
	"encoding/json"
	"strings"
)

const (
	inputCostPerMTok  = 5.00
	outputCostPerMTok = 25.00
)

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

// costUSD computes the cost of a call given token usage at Opus 4.8 pricing.
func costUSD(inputTokens, outputTokens int64) float64 {
	return float64(inputTokens)/1e6*inputCostPerMTok + float64(outputTokens)/1e6*outputCostPerMTok
}
