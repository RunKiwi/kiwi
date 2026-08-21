// ee/slackapp/signature.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature checks Slack's request signature: HMAC-SHA256 over
// "v0:{timestamp}:{body}", keyed on the app's signing secret. Slack's own
// spec: https://api.slack.com/authentication/verifying-requests-from-slack.
func VerifySignature(secret, timestamp string, body []byte, signatureHeader string) bool {
	if !strings.HasPrefix(signatureHeader, "v0=") {
		return false
	}
	want := hmacSHA256(secret, "v0:"+timestamp+":"+string(body))
	got := strings.TrimPrefix(signatureHeader, "v0=")
	return hmac.Equal([]byte(want), []byte(got))
}

func hmacSHA256(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
