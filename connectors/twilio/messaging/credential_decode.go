// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

const (
	// AuthTokenAuthMethodID selects Account SID and Auth Token authentication.
	// The Basic username is the connection's accountSid and the password is auth_token.
	AuthTokenAuthMethodID = "auth-token"
	// APIKeyAuthMethodID selects API key authentication.
	// The Basic username is api_key_sid and the password is api_key_secret.
	APIKeyAuthMethodID = "api-key"
)

var errResolvedCredentialInvalid = errors.New("Twilio resolved credential is invalid")

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential that a
// trusted hosted broker returns, for use as a hostedconfig.CredentialDecoder.
//
// The JSON object holds auth_method and the selected method's fields:
// auth_token, or api_key_sid with api_key_secret. When auth_method is absent,
// the one secret present selects the method. Unknown fields, both secrets, or
// neither secret are rejected, and the error never repeats a secret. The
// account SID is non-secret connection configuration, not broker output.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AuthToken    string `json:"auth_token"`
		APIKeySID    string `json:"api_key_sid"`
		APIKeySecret string `json:"api_key_secret"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	hasAuthToken, hasAPIKeySecret := fields.AuthToken != "", fields.APIKeySecret != ""
	if hasAuthToken == hasAPIKeySecret {
		return Credentials{}, errResolvedCredentialInvalid
	}
	authMethodID := fields.AuthMethodID
	if authMethodID == "" && hasAuthToken {
		authMethodID = AuthTokenAuthMethodID
	}
	if authMethodID == "" && hasAPIKeySecret {
		authMethodID = APIKeyAuthMethodID
	}
	credentials := Credentials{
		AuthMethodID: authMethodID,
		AuthToken:    sdkgo.NewSecretString(fields.AuthToken),
		APIKeySID:    fields.APIKeySID,
		APIKeySecret: sdkgo.NewSecretString(fields.APIKeySecret),
	}
	if err := credentials.Validate(); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	return credentials, nil
}
