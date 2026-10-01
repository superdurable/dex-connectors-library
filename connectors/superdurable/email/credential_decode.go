// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

var errResolvedCredentialInvalid = errors.New("email resolved credential is invalid")

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential that a trusted hosted broker
// returns, for use as a hostedconfig.CredentialDecoder.
//
// The JSON object holds username and password, and optionally smtp_username and smtp_password.
// Unknown fields, a missing required field, or a value with control characters are rejected, and the
// error never repeats a value. Hosts, ports, TLS modes, and the sender are non-secret connection
// configuration, not broker output.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		SMTPUsername string `json:"smtp_username"`
		SMTPPassword string `json:"smtp_password"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	credentials := Credentials{
		Username: fields.Username, Password: sdkgo.NewSecretString(fields.Password),
		SMTPUsername: fields.SMTPUsername, SMTPPassword: sdkgo.NewSecretString(fields.SMTPPassword),
	}
	if validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errResolvedCredentialInvalid
	}
	return credentials, nil
}
