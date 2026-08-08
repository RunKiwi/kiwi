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
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// DiscoveredModel is one model as a provider described it. Every optional field
// is a pointer because unknown and zero mean different things: a nil price
// makes a model unpriceable and therefore never Kiwi-funded, while a zero price
// means genuinely free.
type DiscoveredModel struct {
	ID             string
	DisplayName    string
	Description    string
	InputCostPerM  *float64
	OutputCostPerM *float64
	ContextLength  *int
	SupportsTools  *bool
	Modality       string
}

// Lister fetches a provider's model list.
//
// endpoint is the FULL URL, composed by the caller from the registry's BaseURL
// and ModelsPath. Listers must not append a path of their own: the base URLs
// already carry their version prefix, so a lister that added "/v1/models" to
// OpenAI's ".../v1" requested "/v1/v1/models" and every refresh 404'd.
//
// Implementations must return an error rather than a partial list on any
// transport, status, or parse failure: the refresher treats an error as "change
// nothing", and a silently-short list would look like models disappearing.
type Lister interface {
	List(ctx context.Context, endpoint, apiKey string) ([]DiscoveredModel, error)
}

// httpClient is shared by every lister. The timeout is generous because these
// run on a background refresh where slow is fine and hanging is not.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// getJSON issues a GET and decodes a JSON body, treating any non-200 as an
// error so a provider outage never reaches the refresher looking like an empty
// model list.
func getJSON(ctx context.Context, url string, headers map[string]string, into interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("%s: decode: %w", url, err)
	}
	return nil
}

// EndpointFor composes a provider's discovery URL from its registry row.
func EndpointFor(spec provider.Spec) string {
	return strings.TrimRight(spec.BaseURL, "/") + "/" + strings.TrimLeft(spec.ModelsPath, "/")
}

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }
func ptrB(v bool) *bool       { return &v }
