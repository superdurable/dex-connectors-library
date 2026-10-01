// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

var errResolvedCredentialInvalid = errors.New("Intercom resolved credential is invalid")

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential that a trusted hosted broker
// returns, for use as a hostedconfig.CredentialDecoder.
//
// The JSON object holds access_token and, when the connection receives webhooks, client_secret. Unknown
// fields, a missing access token, or a token that cannot travel in an HTTP header are rejected, and the
// error never repeats either value. The region is non-secret connection configuration, not broker output.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AccessToken  string `json:"access_token"`
		ClientSecret string `json:"client_secret"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString(fields.AccessToken), ClientSecret: sdkgo.NewSecretString(fields.ClientSecret)}
	if validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	return credentials, nil
}
