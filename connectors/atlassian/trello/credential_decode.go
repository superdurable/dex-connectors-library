// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential a trusted hosted broker returns,
// {"api_key": "...", "token": "..."}, holding the Trello API key and the user token. Unknown members are
// rejected, and errors never repeat a value.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	credentials, err := decodeLocalCredentials(contents)
	if err != nil {
		return Credentials{}, errors.New("Trello resolved credential is invalid")
	}
	return credentials, validateResolvedCredentials(credentials)
}

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
