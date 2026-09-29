// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"encoding/json"
	"fmt"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// DecodeResolvedCredentialsJSON decodes operation-scoped credentials returned by a trusted hosted broker.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		SecretKey     string `json:"secret_key"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if err := localconfig.DecodeCredentials(contents, &fields); err != nil {
		return Credentials{}, err
	}
	credentials := Credentials{
		SecretKey:     sdkgo.NewSecretString(fields.SecretKey),
		WebhookSecret: sdkgo.NewSecretString(fields.WebhookSecret),
	}
	if credentials.SecretKey.Reveal() == "" && credentials.WebhookSecret.Reveal() == "" {
		return Credentials{}, fmt.Errorf("resolved Stripe credential is empty")
	}
	return credentials, nil
}
