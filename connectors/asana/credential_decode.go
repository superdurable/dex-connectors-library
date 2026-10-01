// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential a trusted hosted broker returns,
// {"access_token": "..."} holding the personal access token. Unknown members are rejected.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	credentials, err := decodeLocalCredentials(contents)
	if err != nil {
		return Credentials{}, errors.New("Asana resolved credential is invalid")
	}
	return credentials, validateResolvedCredentials(credentials)
}

// validateResolvedCredentials accepts a token that can authorize one request without breaking its header.
func validateResolvedCredentials(credentials Credentials) error {
	accessToken := credentials.AccessToken.Reveal()
	if accessToken == "" {
		return errors.New("Asana personal access token is required")
	}
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return errors.New("Asana personal access token is not a valid header value")
	}
	return nil
}
