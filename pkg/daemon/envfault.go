package daemon

import (
	"regexp"
	"strings"
)

// Environment faults: telling "the sandbox is wrong" apart from "the code is
// wrong", using what the sandbox itself printed.
//
// A test command that cannot run looks exactly like a test command that failed
// — both are a non-zero exit with output. That conflation is what let a Next.js
// task burn its entire budget: `npm test` in a Go image exits 127 "not found",
// the loop read that as a failing test, and six Actor steps went into editing
// code so that a binary which did not exist would succeed.
//
// These errors are legible, and some of them state the fix outright ("go.mod
// requires go >= 1.25.0"). Classifying them lets the daemon repair the sandbox
// and re-run BEFORE the Actor is asked for anything, which is what keeps the
// product's promise — the user configures nothing, and Kiwi notices when its
// own guess was wrong.
//
// The same shape already exists for provider errors in provider.Classify: match
// on what the tool actually prints, return something actionable.

// envFault describes a sandbox that could not run the command, and how to fix
// it. A nil *envFault means the output is a genuine test result.
type envFault struct {
	// Kind is a stable label for logs: missing_runtime | version_mismatch.
	Kind string
	// Image is the corrected image to retry with, or "" when the fault is real
	// but not repairable by swapping images (missing dependencies, say).
	Image string
	// Detail is a short human-readable reason, surfaced on the task when a
	// repair is impossible.
	Detail string
}

// "sh: npm: not found", "bash: line 1: npm: command not found",
// "/bin/sh: 1: npm: not found"
var notFoundRe = regexp.MustCompile(`(?m)([\w.+-]+): (?:command )?not found`)

// "go.mod requires go >= 1.25.0 (running go 1.21.13)"
var goVersionRe = regexp.MustCompile(`go\.mod requires go >= (\d+)\.(\d+)`)

// classifyEnvOutput inspects a failing command's output and reports whether the
// environment — rather than the code — is at fault.
//
// It is deliberately conservative. A false positive re-runs the test in a
// different image, which costs time and could mask a real failure, so each
// pattern matches text only a broken environment produces. Anything unrecognised
// returns nil and is treated as an honest test failure.
func classifyEnvOutput(output string) *envFault {
	if m := goVersionRe.FindStringSubmatch(output); m != nil {
		want := m[1] + "." + m[2]
		return &envFault{
			Kind:   "version_mismatch",
			Image:  goImage(want),
			Detail: "the repository requires Go " + want,
		}
	}

	for _, m := range notFoundRe.FindAllStringSubmatch(output, -1) {
		cmd := m[1]
		eco, ok := commandEcosystem[cmd]
		if !ok {
			continue
		}
		// The missing executable names the toolchain that should have been
		// there, which is a more reliable signal than any file in the repo:
		// whatever we inferred, the command has just proved it wrong.
		return &envFault{
			Kind:   "missing_runtime",
			Image:  imageFor(eco, ""),
			Detail: cmd + " is not available in the sandbox image",
		}
	}

	return nil
}

// correctedImage returns an image to retry with, or "" when the output is a
// genuine test failure or the repair would be a no-op.
//
// dir lets the corrected image pick up a version the repository pins, so a
// missing `node` becomes the project's own Node major rather than the default.
func correctedImage(current, output, dir string) (string, string) {
	fault := classifyEnvOutput(output)
	if fault == nil || fault.Image == "" {
		return "", ""
	}

	next := fault.Image
	// missing_runtime resolves the image without repo context; re-resolve it
	// against the checkout so a pinned version wins over the default.
	if fault.Kind == "missing_runtime" && dir != "" {
		if eco, ok := ecosystemOfImage(next); ok {
			next = imageFor(eco, dir)
		}
	}
	if next == current {
		// Already running it. Retrying would fail identically, so treat the
		// output as a real failure rather than loop on it.
		return "", ""
	}
	return next, fault.Detail
}

// Network reachability failures, as each ecosystem words them.
var networkErrorPatterns = []string{
	"network is unreachable",
	"temporary failure in name resolution",
	"could not resolve host",
	"getaddrinfo eai_again",
	"getaddrinfo enotfound",
	"no such host",
	"failed to fetch",
	"enetunreach",
}

// networkRequired reports why a repository's verification cannot run offline,
// or "" when the failure is something else.
//
// Some projects need the network to *build*, not merely to install: this repo's
// own website imports next/font/google, and Next fetches the font on every
// build, cache or no cache. Phase A cannot help — the fetch is part of the
// build, and the build is the thing that must run without network because it
// executes model-generated code.
//
// There is no fix to apply here, so the point is to say so. The alternative is
// what happened before: six Actor steps editing a component in response to a
// font download failure, the user's whole budget spent, and a task that reports
// only that the test did not pass.
//
// This is only consulted for the FIRST verification run, before any edit has
// been applied. At that point nothing model-generated has executed, so a
// network failure is a property of the repository rather than of the agent —
// which is what makes the classification safe.
func networkRequired(output string) string {
	lower := strings.ToLower(output)
	found := false
	for _, p := range networkErrorPatterns {
		if strings.Contains(lower, p) {
			found = true
			break
		}
	}
	if !found {
		return ""
	}

	// Name the cause when it is recognisable, so the message is actionable
	// rather than merely accurate.
	switch {
	case strings.Contains(lower, "font/google"), strings.Contains(lower, "fonts.googleapis"):
		return "this project's build downloads Google Fonts (next/font/google), and network access is disabled while running model-generated code. " +
			"Use a test command that runs offline, or self-host the font."
	case strings.Contains(lower, "npm err"), strings.Contains(lower, "yarn"):
		return "this project's test command reaches the network, which is disabled while running model-generated code. " +
			"Dependencies are installed beforehand; anything fetched during the test itself cannot be."
	}
	return "this project's verification needs network access, which is disabled while running model-generated code. " +
		"Dependencies are installed beforehand — a test command that fetches at run time cannot work here."
}

// ecosystemOfImage recovers the toolchain from an image reference, so a
// correction can be re-resolved against the repository's pinned version.
func ecosystemOfImage(image string) (ecosystem, bool) {
	switch {
	case strings.HasPrefix(image, "golang:"):
		return ecoGo, true
	case strings.HasPrefix(image, "node:"):
		return ecoNode, true
	case strings.HasPrefix(image, "python:"):
		return ecoPython, true
	case strings.HasPrefix(image, "rust:"):
		return ecoRust, true
	case strings.HasPrefix(image, "maven:"):
		return ecoMaven, true
	case strings.HasPrefix(image, "gradle:"):
		return ecoGradle, true
	case strings.HasPrefix(image, "ruby:"):
		return ecoRuby, true
	case strings.HasPrefix(image, "php:"):
		return ecoPHP, true
	}
	return "", false
}
