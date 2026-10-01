// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

var errResolvedCredentialInvalid = errors.New("Freshdesk resolved credential is invalid")

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential that a
// trusted hosted broker returns, for use as a hostedconfig.CredentialDecoder.
//
// The JSON object holds exactly api_key. Unknown fields, a missing key, or a key
// that cannot travel as an HTTP Basic user name are rejected, and the error never
// repeats the value. The domain is non-secret connection configuration, not broker
// output.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		APIKey string `json:"api_key"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString(fields.APIKey)}
	if validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	return credentials, nil
}
