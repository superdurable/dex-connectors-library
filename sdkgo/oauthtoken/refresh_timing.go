// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import "time"

// RefreshSkew is how long before expiry an access token counts as expired, so
// an operation never starts with a token that can expire during the call.
const RefreshSkew = 5 * time.Minute

// MissingExpiryRule decides whether an access token without a recorded expiry
// needs a refresh.
type MissingExpiryRule int

const (
	// RefreshWhenExpiryMissing refreshes the token. Use it when the provider
	// issues only expiring access tokens, such as Google.
	RefreshWhenExpiryMissing MissingExpiryRule = iota + 1
	// KeepWhenExpiryMissing keeps the token as the provider's supported
	// non-expiring form, such as a GitHub Enterprise or non-rotating Slack token.
	KeepWhenExpiryMissing
)

// IsRefreshRequired reports whether an access token must be refreshed before
// use at now: it is absent, it has no expiry and rule says to refresh, or it
// expires within RefreshSkew. It is deterministic for one input, as
// sdkgo.CredentialRefreshDriver.RefreshRequired requires.
func IsRefreshRequired(hasAccessToken bool, expiresAt *time.Time, now time.Time, rule MissingExpiryRule) bool {
	if !hasAccessToken {
		return true
	}
	if expiresAt == nil {
		return rule != KeepWhenExpiryMissing
	}
	return !expiresAt.After(now.Add(RefreshSkew))
}
