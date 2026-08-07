// Package catalog discovers which models a provider serves and keeps the
// model_catalog table current.
//
// Discovery exists because the alternative is a hand-maintained list that is
// wrong the day a provider ships a model. Providers differ sharply in what
// their list endpoints reveal: OpenRouter returns pricing and capability, and
// the rest return little more than ids. That asymmetry is why OpenRouter is the
// provider Kiwi funds and the others are BYOK-first.
package catalog

import (
	"context"
	"net/http"
	"time"
)

// DiscoveredModel is one model as a provider described it. Every optional field
// is a pointer because unknown and zero mean different things: a nil price
// makes a model unpriceable and therefore never Kiwi-funded, while a zero price
// means genuinely free.
type DiscoveredModel struct {
	ID             string
	DisplayName    string
	InputCostPerM  *float64
	OutputCostPerM *float64
	ContextLength  *int
	SupportsTools  *bool
	Modality       string
}

// Lister fetches a provider's model list. Implementations must return an error
// rather than a partial list on any transport, status, or parse failure: the
// refresher treats an error as "change nothing", and a silently-short list
// would look like models disappearing.
type Lister interface {
	List(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error)
}

// httpClient is shared by every lister. The timeout is generous because these
// run on a background refresh where slow is fine and hanging is not.
var httpClient = &http.Client{Timeout: 30 * time.Second}

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }
func ptrB(v bool) *bool       { return &v }
