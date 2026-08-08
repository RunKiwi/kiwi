package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const openRouterBody = `{"data":[
  {"id":"moonshotai/kimi-k2","name":"MoonshotAI: Kimi K2","context_length":131072,
   "architecture":{"modality":"text->text"},
   "pricing":{"prompt":"0.0000006","completion":"0.0000025"},
   "supported_parameters":["tools","tool_choice","max_tokens"]},
  {"id":"deepseek/deepseek-chat-v3:free","name":"DeepSeek V3 (free)","context_length":163840,
   "architecture":{"modality":"text->text"},
   "pricing":{"prompt":"0","completion":"0"},
   "supported_parameters":["tools","max_tokens"]},
  {"id":"openai/gpt-4o-mini-tts","name":"GPT-4o mini TTS","context_length":2048,
   "architecture":{"modality":"text->audio"},
   "pricing":{"prompt":"0.0000006","completion":"0.000012"},
   "supported_parameters":["max_tokens"]}
]}`

func TestOpenRouterListerParsesPricingAndCapability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("requested %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(openRouterBody))
	}))
	defer srv.Close()

	got, err := (OpenRouterLister{}).List(context.Background(), srv.URL+"/models", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d models, want 3 (filtering happens in derivation, not here)", len(got))
	}

	byID := map[string]DiscoveredModel{}
	for _, m := range got {
		byID[m.ID] = m
	}

	// Pricing arrives as USD per *token* as a string; the catalog stores USD
	// per million. Getting this conversion wrong is a factor-of-a-million
	// pricing error, which is why it is asserted exactly.
	kimi := byID["moonshotai/kimi-k2"]
	if kimi.InputCostPerM == nil || *kimi.InputCostPerM != 0.60 {
		t.Errorf("kimi InputCostPerM = %v, want 0.60", kimi.InputCostPerM)
	}
	if kimi.OutputCostPerM == nil || *kimi.OutputCostPerM != 2.50 {
		t.Errorf("kimi OutputCostPerM = %v, want 2.50", kimi.OutputCostPerM)
	}
	if kimi.ContextLength == nil || *kimi.ContextLength != 131072 {
		t.Errorf("kimi ContextLength = %v, want 131072", kimi.ContextLength)
	}
	if kimi.SupportsTools == nil || !*kimi.SupportsTools {
		t.Errorf("kimi SupportsTools = %v, want true", kimi.SupportsTools)
	}
	if kimi.Modality != "text->text" {
		t.Errorf("kimi Modality = %q", kimi.Modality)
	}
	if kimi.DisplayName != "MoonshotAI: Kimi K2" {
		t.Errorf("kimi DisplayName = %q", kimi.DisplayName)
	}

	// A genuinely free model prices at zero, which must be preserved as zero
	// and not confused with unknown.
	free := byID["deepseek/deepseek-chat-v3:free"]
	if free.InputCostPerM == nil || *free.InputCostPerM != 0 {
		t.Errorf("free model InputCostPerM = %v, want 0", free.InputCostPerM)
	}
	if free.OutputCostPerM == nil || *free.OutputCostPerM != 0 {
		t.Errorf("free model OutputCostPerM = %v, want 0", free.OutputCostPerM)
	}

	tts := byID["openai/gpt-4o-mini-tts"]
	if tts.SupportsTools == nil || *tts.SupportsTools {
		t.Errorf("tts SupportsTools = %v, want false", tts.SupportsTools)
	}
	if tts.Modality != "text->audio" {
		t.Errorf("tts Modality = %q", tts.Modality)
	}
}

func TestOpenRouterListerRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if _, err := (OpenRouterLister{}).List(context.Background(), srv.URL+"/models", ""); err == nil {
		t.Fatal("List returned nil error on a 503")
	}
}

func TestOpenRouterListerRejectsMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	if _, err := (OpenRouterLister{}).List(context.Background(), srv.URL+"/models", ""); err == nil {
		t.Fatal("List returned nil error on a malformed body")
	}
}

// An unparseable price is not a zero price. It must come back nil so the model
// lands in tier "unknown" and is never Kiwi-funded.
func TestOpenRouterListerLeavesBadPricingNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"x","context_length":100000,
		  "architecture":{"modality":"text->text"},
		  "pricing":{"prompt":"","completion":"-1"},
		  "supported_parameters":["tools"]}]}`))
	}))
	defer srv.Close()

	got, err := (OpenRouterLister{}).List(context.Background(), srv.URL+"/models", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got[0].InputCostPerM != nil {
		t.Errorf("empty price parsed to %v, want nil", got[0].InputCostPerM)
	}
	if got[0].OutputCostPerM != nil {
		t.Errorf("negative price parsed to %v, want nil", got[0].OutputCostPerM)
	}
}

// Descriptions come from the provider, not from Kiwi. They are normalised and
// bounded because they are rendered inside a dropdown panel, where an
// unbounded paragraph is worse than showing nothing.
func TestTruncateDescription(t *testing.T) {
	if got := truncateDescription("  a   b \n c  "); got != "a b c" {
		t.Errorf("whitespace not collapsed: %q", got)
	}
	if got := truncateDescription(""); got != "" {
		t.Errorf("empty description became %q", got)
	}

	short := "A compact model for everyday work."
	if got := truncateDescription(short); got != short {
		t.Errorf("a short description was altered: %q", got)
	}

	long := strings.Repeat("alpha beta ", 200)
	got := truncateDescription(long)
	if len([]rune(got)) > maxDescriptionLen+1 {
		t.Errorf("length %d exceeds the cap", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated description does not end with an ellipsis: %q", got[len(got)-20:])
	}
	// Cut on a word boundary, so the ellipsis never lands mid-word.
	if strings.Contains(got, "alp…") || strings.Contains(got, "bet…") {
		t.Errorf("cut mid-word: %q", got[len(got)-20:])
	}
}

func TestOpenRouterListerCapturesDescription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"x","name":"X","context_length":100000,
		  "description":"A  large\nMoE model for coding.",
		  "architecture":{"modality":"text->text"},
		  "pricing":{"prompt":"0.0000006","completion":"0.0000025"},
		  "supported_parameters":["tools"]}]}`))
	}))
	defer srv.Close()

	got, err := (OpenRouterLister{}).List(context.Background(), srv.URL+"/models", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got[0].Description != "A large MoE model for coding." {
		t.Errorf("Description = %q", got[0].Description)
	}
}
