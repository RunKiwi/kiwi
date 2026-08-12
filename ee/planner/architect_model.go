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
func (s *Service) architectModelFor(ctx context.Context, req PlanRequest) string {
	if req.ArchitectModel != "" {
		return req.ArchitectModel
	}
	candidate := os.Getenv("KIWI_ARCHITECT_MODEL")
	if candidate == "" {
		candidate = DefaultArchitectModel
	}
	// Nothing to buy: the default IS the Implementer, so the two-model split
	// would cost an extra resolution to arrive back where it started.
	if candidate == "" || candidate == req.Model || req.Model == "" {
		return ""
	}

	implFunding, err := s.fundingFor(ctx, req.OrgID, req.Model)
	if err != nil {
		return ""
	}
	archFunding, err := s.fundingFor(ctx, req.OrgID, candidate)
	if err != nil {
		return ""
	}
	if implFunding != archFunding {
		return ""
	}
	if err := s.requireEntitlement(ctx, req.OrgID, req.FleetID, candidate); err != nil {
		return ""
	}
	// Kiwi supplies the key for a Kiwi-funded model, so there is nothing for the
	// org to have connected. Otherwise the key has to already be there.
	if archFunding != store.FundingKiwi {
		if err := s.requireProviderKey(ctx, req.OrgID, s.providerOf(ctx, req.OrgID, candidate)); err != nil {
			return ""
		}
	}
	return candidate
}

// providerOf resolves the provider that serves a model, preferring the catalog
// (which knows an aggregator's ids) and falling back to prefix inference.
func (s *Service) providerOf(ctx context.Context, orgID, model string) string {
	if res, err := s.store.ResolveModel(ctx, orgID, model); err == nil && res.Provider != "" {
		return res.Provider
	}
	return provider.ProviderOf(model)
}
