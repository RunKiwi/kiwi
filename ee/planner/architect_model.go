// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// DefaultArchitectModel is the model that plans and reviews when a submit does
// not name one.
//
// It exists because the previous answer was "whatever the Implementer runs",
// and that quietly cancelled the design. The Architect is called a handful of
// times per task and the Implementer constantly, so the split buys judgment
// where it is cheap to buy — but only if the two are actually different models.
// The dashboard defaulted the Architect field to empty, the daemon fell back to
// the worker's model, and the worker default is a small fast model: every task
// submitted without touching that field ran a small model planning blind and
// the same small model implementing. The expensive half of the split was never
// bought, and nothing said so.
const DefaultArchitectModel = "claude-opus-4-8"

// DefaultWorkerModel is the last-resort Implementer model a submit runs when
// it names none and no Kiwi-funded model could be found — kept in lockstep
// with the dashboard's own DEFAULT_WORKER_MODEL (frontend/src/lib/api.ts) so
// every entry point defaults to the same run. It requires the org's own
// Anthropic key, so defaultWorkerModelFor below only reaches it when the
// runtime catalog lookup comes up empty (no OpenRouter platform key
// configured on this deployment, or nothing in that catalog is currently
// affordable for the org) — see defaultWorkerModelFor.
//
// Before either existed, an empty req.Model reached SubmitPlan unchanged: the
// per-worker entitlement/funding check at service.go's admission loop skips
// anything named "" (nothing to check), architectModelFor's own early-out
// treats req.Model == "" as "nothing to buy an Architect split for" and
// leaves ArchitectModel empty too, and the daemon's defaultProvider ends up
// calling a provider with model id "". Every one of those exists precisely
// through Slack, which never set Model, and would keep existing through any
// future caller that also doesn't — an unset model is not a valid submit,
// so it must never reach any of that code as one.
const DefaultWorkerModel = "claude-haiku-4-5-20251001"

// defaultWorkerModelFor is what SubmitPlan calls for an unset req.Model.
//
// A Kiwi-funded model is tried first, not as a fallback: it needs no key
// from the org at all (Kiwi's own platform key pays), which is the only kind
// of default that works for an org that has connected nothing of its own —
// the common case for a Slack trigger nobody has pointed at a specific model.
// requireEntitlement is the same check used everywhere else in this file, so
// a model that would immediately fail on an exhausted allowance is never
// handed back — this is a live check against the org's remaining budget, not
// a cached "Kiwi funds this" flag that could be stale by the time the task
// runs.
//
// DefaultWorkerModel — BYOK, requires the org's own Anthropic key — is the
// fallback for everything the catalog lookup does not cover: no OpenRouter
// platform key on this deployment, no economy-tier OpenRouter model
// discovered yet, or the org's Kiwi-token allowance is exhausted.
// The returned warning is non-empty only when a Kiwi-funded candidate exists
// but the org's tier allowance is exhausted — the case worth telling the org
// about, as opposed to "no OpenRouter platform key on this deployment" or "no
// qualifying model in the catalog yet," which are deployment facts, not
// something happening to this org's usage.
func (s *Service) defaultWorkerModelFor(ctx context.Context, orgID, fleetID string) (model, warning string) {
	// An org that opted into store.ModelSourceBYOK skips the Kiwi-funded
	// cascade entirely — this is the one place that matters: architectModelFor
	// never sees a Kiwi-funded req.Model to build a split on top of, so it
	// falls through to its own BYOK default without needing this preference
	// checked a second time.
	if src, serr := s.store.ModelSource(ctx, orgID); serr == nil && src == store.ModelSourceBYOK {
		return DefaultWorkerModel, ""
	}
	// requireEntitlement returns nil for two different reasons: the pick is
	// genuinely fine, OR Kiwi holds no platform key for the provider at all
	// (service.go's "nothing to fund, the org can run this on their own
	// key" case) — which is nil for a caller passing in an explicit model
	// the org chose, but wrong here: this candidate came from
	// CheapestKiwiFundedModel specifically because the org has connected
	// nothing of its own, so a missing platform key must fall through to
	// DefaultWorkerModel, not be accepted as an unusable pick.
	if _, ok := provider.PlatformKeyFor(provider.ProviderOpenRouter); !ok {
		return DefaultWorkerModel, ""
	}
	candidate, ok, err := s.store.CheapestKiwiFundedModel(ctx, orgID, provider.ProviderOpenRouter, store.TierEconomy)
	if err == nil && ok {
		if s.requireEntitlement(ctx, orgID, fleetID, candidate) == nil {
			return candidate, ""
		}
		return DefaultWorkerModel, "Kiwi-funded models are at capacity for your plan right now — this task ran on the default instead."
	}
	return DefaultWorkerModel, ""
}

// architectModelFor decides which model plans and reviews this request.
//
// The rule that matters is the one about NOT applying the default. A value the
// user did not ask for must never turn a submit that would have succeeded into
// one that fails, and there are two ways it could:
//
//   - Funding. SubmitPlan refuses a task whose Architect and Implementer are
//     paid for differently, because a task records one payer and a split one
//     would be unattributable. Injecting a Kiwi-provided Architect over a BYOK
//     Implementer would manufacture exactly that refusal.
//   - Entitlement. The org may have no allowance for the default model even
//     when the funding source lines up.
//   - A key for the Architect's provider. The default is an Anthropic model; an
//     org running a Gemini Implementer on their own Gemini key has no reason to
//     have connected an Anthropic one, and SubmitPlan checks the key for the
//     *Architect's* provider. Injecting the default there would tell that org to
//     go and connect a second provider before a submit that used to work.
//
// In every case the answer is to return empty and let the daemon fall back to
// the Implementer's model — the behaviour before this function existed. A
// weaker Architect is a worse run; a rejected submit is no run at all.
//
// The returned warning is non-empty only for the one case worth telling the
// org about: a Kiwi-funded frontier candidate exists but the org's tier
// allowance is exhausted. Every other empty-model case (no operator/explicit
// choice needed, no catalog candidate, funding mismatch, missing key) is a
// normal "nothing to buy" outcome, not something happening to this org's
// usage.
func (s *Service) architectModelFor(ctx context.Context, req PlanRequest) (model, warning string) {
	if req.ArchitectModel != "" {
		return req.ArchitectModel, ""
	}
	if req.Model == "" {
		return "", ""
	}
	implFunding, err := s.fundingFor(ctx, req.OrgID, req.Model)
	if err != nil {
		return "", ""
	}

	// No operator override and a Kiwi-funded Implementer: prefer a Kiwi-funded
	// frontier model over the BYOK default below. Same payer as the
	// Implementer (no funding-mismatch refusal possible), no extra provider
	// key required — the case that matters most is a zero-config Slack
	// trigger, whose Implementer already landed on defaultWorkerModelFor's
	// Kiwi-funded economy pick.
	if os.Getenv("KIWI_ARCHITECT_MODEL") == "" && implFunding == store.FundingKiwi {
		candidate, ok, cerr := s.store.CheapestKiwiFundedModel(ctx, req.OrgID, provider.ProviderOpenRouter, store.TierFrontier)
		if cerr == nil && ok && candidate != req.Model {
			if s.requireEntitlement(ctx, req.OrgID, req.FleetID, candidate) == nil {
				return candidate, ""
			}
			return "", "Kiwi-funded Architect models are at capacity for your plan right now — this task ran without a separate planning/review model."
		}
		return "", ""
	}

	candidate := os.Getenv("KIWI_ARCHITECT_MODEL")
	if candidate == "" {
		candidate = DefaultArchitectModel
	}
	// Nothing to buy: the default IS the Implementer, so the two-model split
	// would cost an extra resolution to arrive back where it started.
	if candidate == "" || candidate == req.Model {
		return "", ""
	}
	archFunding, err := s.fundingFor(ctx, req.OrgID, candidate)
	if err != nil {
		return "", ""
	}
	if implFunding != archFunding {
		return "", ""
	}
	if err := s.requireEntitlement(ctx, req.OrgID, req.FleetID, candidate); err != nil {
		return "", ""
	}
	// Kiwi supplies the key for a Kiwi-funded model, so there is nothing for the
	// org to have connected. Otherwise the key has to already be there.
	if archFunding != store.FundingKiwi {
		if err := s.requireProviderKey(ctx, req.OrgID, s.providerOf(ctx, req.OrgID, candidate)); err != nil {
			return "", ""
		}
	}
	return candidate, ""
}

// providerOf resolves the provider that serves a model, preferring the catalog
// (which knows an aggregator's ids) and falling back to prefix inference.
func (s *Service) providerOf(ctx context.Context, orgID, model string) string {
	if res, err := s.store.ResolveModel(ctx, orgID, model); err == nil && res.Provider != "" {
		return res.Provider
	}
	return provider.ProviderOf(model)
}
