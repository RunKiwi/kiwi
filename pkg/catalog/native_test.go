package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNativeListersExtractIDs(t *testing.T) {
	cases := []struct {
		name   string
		lister Lister
		body   string
		want   []string
	}{
		{
			name:   "OpenAI",
			lister: OpenAILister{},
			body:   `{"data": [{"id": "gpt-4o"}, {"id": "gpt-3.5-turbo"}]}`,
			want:   []string{"gpt-4o", "gpt-3.5-turbo"},
		},
		{
			name:   "Anthropic",
			lister: AnthropicLister{},
			body:   `{"data": [{"id": "claude-3-opus"}, {"id": "claude-3-haiku"}]}`,
			want:   []string{"claude-3-opus", "claude-3-haiku"},
		},
		{
			name:   "Gemini",
			lister: GeminiLister{},
			body:   `{"models": [{"name": "models/gemini-1.5-pro"}, {"name": "models/gemini-1.5-flash"}]}`,
			want:   []string{"gemini-1.5-pro", "gemini-1.5-flash"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := tc.lister.List(context.Background(), srv.URL, "fake-key")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d models, want %d", len(got), len(tc.want))
			}
			for i, m := range got {
				if m.ID != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, m.ID, tc.want[i])
				}
			}
		})
	}
}

func TestEnrichFromPricingMap(t *testing.T) {
	// A mock pricing map covering just the models we test here.
	pricing := map[string]struct {
		InputCostPerM  float64
		OutputCostPerM float64
	}{
		"gpt-4o":        {5.00, 15.00},
		"claude-3-opus": {15.00, 75.00},
	}
	lookup := func(id string) (*float64, *float64) {
		if p, ok := pricing[id]; ok {
			return ptrF(p.InputCostPerM), ptrF(p.OutputCostPerM)
		}
		return nil, nil
	}

	raw := []DiscoveredModel{
		{ID: "gpt-4o"},
		{ID: "unknown-new-model"},
	}

	got := EnrichFromPricingMap(raw, lookup)
	if len(got) != 2 {
		t.Fatalf("got %d models, want 2", len(got))
	}

	m0 := got[0]
	if m0.ID != "gpt-4o" {
		t.Errorf("got[0].ID = %q, want gpt-4o", m0.ID)
	}
	if m0.InputCostPerM == nil || *m0.InputCostPerM != 5.00 {
		t.Errorf("got[0].InputCostPerM = %v, want 5.00", m0.InputCostPerM)
	}
	if m0.OutputCostPerM == nil || *m0.OutputCostPerM != 15.00 {
		t.Errorf("got[0].OutputCostPerM = %v, want 15.00", m0.OutputCostPerM)
	}
	// Missing from the map leaves pricing nil, so it evaluates to TierUnknown.
	m1 := got[1]
	if m1.InputCostPerM != nil || m1.OutputCostPerM != nil {
		t.Errorf("unknown model got pricing %v / %v, want nil", m1.InputCostPerM, m1.OutputCostPerM)
	}
}
