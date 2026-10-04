// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// validateResolvedCredentials accepts a key and token that fit inside the quoted Authorization header.
func validateResolvedCredentials(credentials Credentials) error {
	if !isQuotedHeaderSafe(credentials.APIKey.Reveal()) {
		return errors.New("Trello API key is required and must be printable ASCII without spaces, quotes, commas, or backslashes")
	}
	if !isQuotedHeaderSafe(credentials.Token.Reveal()) {
		return errors.New("Trello token is required and must be printable ASCII without spaces, quotes, commas, or backslashes")
	}
	return nil
}

// isQuotedHeaderSafe reports a value that cannot end or split oauth_consumer_key="..." or oauth_token="...".
func isQuotedHeaderSafe(value string) bool {
	return providerhttp.IsHeaderSafeCredential(value) && !strings.ContainsAny(value, `"\,`)
}
