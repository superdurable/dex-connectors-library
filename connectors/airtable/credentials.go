// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential a
// trusted hosted broker returns, {"personal_access_token": "..."}, for
// hostedconfig.NewCredentialProviderFromEnvironment. Unknown members, a blank
// token, or a token that cannot travel in one HTTP header are rejected.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		PersonalAccessToken string `json:"personal_access_token"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, err
	}
	if !providerhttp.IsHeaderSafeCredential(fields.PersonalAccessToken) {
		return Credentials{}, errors.New("resolved Airtable personal access token is missing or malformed")
	}
	return Credentials{PersonalAccessToken: sdkgo.NewSecretString(fields.PersonalAccessToken)}, nil
}
