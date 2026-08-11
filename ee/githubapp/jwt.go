// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

// Package githubapp mints short-lived GitHub App installation tokens.
//
// A GitHub App authenticates in two steps. The App itself proves who it is with
// an RS256 JWT signed by its private key; that JWT then buys an *installation*
// token, scoped to the repositories one customer granted, valid for one hour.
// Only the second token touches a repository, and it is the one Kiwi hands the
// daemon in place of a personal access token.
//
// The distinction matters for what Kiwi can be blamed for. A PAT is a standing
// credential with whatever scope the user happened to grant, usually org-wide
// write, living in Kiwi's database until someone remembers to rotate it. An
// installation token covers only the repositories the customer ticked, expires
// within the hour, and the customer can revoke the whole installation from
// their own settings without telling us.
package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ParsePrivateKey reads the PEM GitHub hands you when an App is created.
//
// GitHub issues PKCS#1 ("RSA PRIVATE KEY"). PKCS#8 ("PRIVATE KEY") is accepted
// too because key management tooling routinely re-wraps it on the way through,
// and failing on that would produce a startup error that blames the key rather
// than the conversion.
func ParsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("githubapp: no PEM block found in private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("githubapp: parse private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("githubapp: private key is %T, want RSA", parsed)
	}
	return key, nil
}

// appJWT signs the assertion that authenticates the App itself.
//
// iat is backdated 60s because GitHub rejects a token whose iat is in the
// future by even a second, and a control plane clock that runs slightly fast is
// otherwise an intermittent, unattributable 401. exp is 9 minutes rather than
// the permitted 10 for the same reason, from the other end.
func appJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	}

	segments := make([]string, 0, 3)
	for _, part := range []any{header, claims} {
		raw, err := json.Marshal(part)
		if err != nil {
			return "", fmt.Errorf("githubapp: marshal jwt segment: %w", err)
		}
		segments = append(segments, base64.RawURLEncoding.EncodeToString(raw))
	}

	signingInput := strings.Join(segments, ".")
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("githubapp: sign jwt: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
