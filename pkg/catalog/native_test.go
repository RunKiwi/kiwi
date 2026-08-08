package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// EndpointFor is what keeps a lister from inventing its own path. The registry
// base URLs already carry their version prefix, so a lister that appended
// "/v1/models" to OpenAI's ".../v1" requested "/v1/v1/models" and every refresh
// 404'd in silence.
func TestEndpointForDoesNotDoublePathPrefixes(t *testing.T) {
	cases := map[string]string{
		provider.ProviderOpenAI:     "https://api.openai.com/v1/models",
		provider.ProviderGemini:     "https://generativelanguage.googleapis.com/v1beta/models",
		provider.ProviderAnthropic:  "https://api.anthropic.com/v1/models",
		provider.ProviderOpenRouter: "https://openrouter.ai/api/v1/models",
	}
	for id, want := range cases {
		spec, ok := provider.SpecFor(id)
		if !ok {
			t.Fatalf("SpecFor(%q) missing", id)
		}
		if got := EndpointFor(spec); got != want {
			t.Errorf("EndpointFor(%s) = %q, want %q", id, got, want)
		}
	}
}

// Each lister must request exactly the URL it was given and send the auth the
// provider requires. Asserting only the parse — as the previous version did —
// leaves a doubled path or a missing header completely invisible.
func TestOpenAIListerRequest(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5-mini"},{"id":"text-embedding-3-small"}]}`))
	}))
	defer srv.Close()

	got, err := OpenAILister{}.List(context.Background(), srv.URL+"/v1/models", "sk-test")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("requested %q, want /v1/models — the lister appended a path of its own", gotPath)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if len(got) != 2 {
		t.Fatalf("got %d models, want 2", len(got))
	}
	// This endpoint reveals nothing but ids. Inventing capability here is how
	// an embedding model reaches the picker.
	if got[0].SupportsTools != nil || got[0].ContextLength != nil {
		t.Error("the OpenAI lister invented capability it was never told")
	}
}

func TestAnthropicListerRequest(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-4-8","display_name":"Claude Opus 4.8"}]}`))
	}))
	defer srv.Close()

	got, err := AnthropicLister{}.List(context.Background(), srv.URL+"/v1/models", "sk-ant")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotPath != "/v1/models" {
		t.Errorf("requested %q, want /v1/models", gotPath)
	}
	if gotKey != "sk-ant" {
		t.Errorf("x-api-key = %q", gotKey)
	}
	// The real API returns 400 on every request without this header.
	if gotVersion == "" {
		t.Error("anthropic-version header missing; the API rejects requests without it")
	}
	if got[0].DisplayName != "Claude Opus 4.8" {
		t.Errorf("DisplayName = %q, want the provider's label", got[0].DisplayName)
	}
}

func TestGeminiListerRequest(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.URL.Query().Get("key")
		_, _ = w.Write([]byte(`{"models":[
		  {"name":"models/gemini-2.0-flash","displayName":"Gemini 2.0 Flash",
		   "inputTokenLimit":1048576,"supportedGenerationMethods":["generateContent"]},
		  {"name":"models/embedding-001","displayName":"Embedding",
		   "inputTokenLimit":2048,"supportedGenerationMethods":["embedContent"]}
		]}`))
	}))
	defer srv.Close()

	got, err := GeminiLister{}.List(context.Background(), srv.URL+"/v1beta/models", "g-key")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotPath != "/v1beta/models" {
		t.Errorf("requested %q, want /v1beta/models", gotPath)
	}
	if gotKey != "g-key" {
		t.Errorf("key query param = %q", gotKey)
	}

	byID := map[string]DiscoveredModel{}
	for _, m := range got {
		byID[m.ID] = m
	}
	// "models/" is a resource path, not a model id. Storing it makes every
	// catalog lookup miss.
	flash, ok := byID["gemini-2.0-flash"]
	if !ok {
		t.Fatalf("gemini-2.0-flash not found; got %+v", byID)
	}
	if flash.ContextLength == nil || *flash.ContextLength != 1048576 {
		t.Errorf("ContextLength = %v, want 1048576", flash.ContextLength)
	}
	if flash.Modality != "text->text" {
		t.Errorf("Modality = %q, want text->text", flash.Modality)
	}
	// An embedding model does not generate content and must not be offered.
	if emb := byID["embedding-001"]; emb.Modality == "text->text" {
		t.Error("an embedding model was reported as text->text")
	}
}

// The native endpoints report no pricing, so the catalog joins to the real
// PricingMap. A model absent from it must stay nil-priced rather than picking
// up a fallback price that belongs to some other model.
func TestEnrichFromPricingMapUsesTheRealTable(t *testing.T) {
	d := DiscoveredModel{ID: "gemini-2.0-flash"}
	EnrichFromPricingMap(provider.ProviderGemini, &d)

	want, ok := provider.PricingMap["gemini-2.0-flash"]
	if !ok {
		t.Fatal("gemini-2.0-flash is not in provider.PricingMap; the fixture needs updating")
	}
	if d.InputCostPerM == nil || *d.InputCostPerM != want.InputCostPerM {
		t.Errorf("InputCostPerM = %v, want %v", d.InputCostPerM, want.InputCostPerM)
	}
	if d.OutputCostPerM == nil || *d.OutputCostPerM != want.OutputCostPerM {
		t.Errorf("OutputCostPerM = %v, want %v", d.OutputCostPerM, want.OutputCostPerM)
	}

	unknown := DiscoveredModel{ID: "some-model-nobody-has-priced"}
	EnrichFromPricingMap(provider.ProviderGemini, &unknown)
	if unknown.InputCostPerM != nil {
		t.Errorf("an unpriced model got price %v; it must stay nil so it is never Kiwi-funded", unknown.InputCostPerM)
	}
}

// A value the provider actually reported always beats the static table.
func TestEnrichFromPricingMapNeverOverwritesProviderData(t *testing.T) {
	d := DiscoveredModel{
		ID:            "gemini-2.0-flash",
		InputCostPerM: ptrF(99), OutputCostPerM: ptrF(98),
		SupportsTools: ptrB(false), Modality: "text->image",
	}
	EnrichFromPricingMap(provider.ProviderGemini, &d)

	if *d.InputCostPerM != 99 || *d.OutputCostPerM != 98 {
		t.Error("a provider-supplied price was overwritten by the static table")
	}
	if *d.SupportsTools {
		t.Error("a provider-supplied capability was overwritten")
	}
	if d.Modality != "text->image" {
		t.Errorf("Modality = %q, want the provider's text->image", d.Modality)
	}
}

func TestEnrichFromPricingMapInfersToolSupport(t *testing.T) {
	tool := DiscoveredModel{ID: "claude-opus-4-8"}
	EnrichFromPricingMap(provider.ProviderAnthropic, &tool)
	if tool.SupportsTools == nil || !*tool.SupportsTools {
		t.Error("a claude- model was not inferred tool-capable")
	}
	if tool.Modality != "text->text" {
		t.Errorf("Modality = %q, want text->text", tool.Modality)
	}

	// Conservative: an id outside the table keeps unknown capability and is
	// therefore not selectable, which is a better failure than offering a model
	// that cannot drive the loop.
	notool := DiscoveredModel{ID: "whisper-1"}
	EnrichFromPricingMap(provider.ProviderOpenAI, &notool)
	if notool.SupportsTools != nil {
		t.Errorf("whisper-1 SupportsTools = %v, want nil", notool.SupportsTools)
	}
}

// A registry provider with no lister can never have its models discovered.
func TestListerForEveryRegistryProvider(t *testing.T) {
	for _, spec := range provider.Registry() {
		if _, ok := ListerFor(spec.ID); !ok {
			t.Errorf("no Lister for registry provider %q; its models can never be discovered", spec.ID)
		}
	}
	if _, ok := ListerFor("not-a-provider"); ok {
		t.Error("ListerFor returned a lister for an unknown provider")
	}
}
