// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

var errResolvedCredentialInvalid = errors.New("Mailchimp resolved credential is invalid")

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential that a trusted hosted
// broker returns, for use as a hostedconfig.CredentialDecoder.
//
// The JSON object holds exactly api_key. Unknown fields, a missing key, or a key without the
// -us6 style data-center suffix that selects the API host are rejected, and the error never
// repeats the value.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		APIKey string `json:"api_key"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString(fields.APIKey)}
	if _, err := validateResolvedCredentials(credentials); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	return credentials, nil
}
