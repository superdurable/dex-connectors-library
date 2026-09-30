// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

var errResolvedCredentialInvalid = errors.New("Zendesk resolved credential is invalid")

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential that a
// trusted hosted broker returns, for use as a hostedconfig.CredentialDecoder.
//
// The JSON object holds exactly email and api_token. Unknown fields, a missing
// field, an email that is not one bare address, or a token that cannot travel in
// an HTTP header are rejected, and the error never repeats either value. The
// subdomain is non-secret connection configuration, not broker output.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		Email    string `json:"email"`
		APIToken string `json:"api_token"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	credentials := Credentials{Email: fields.Email, APIToken: sdkgo.NewSecretString(fields.APIToken)}
	if validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	return credentials, nil
}
