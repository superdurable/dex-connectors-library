// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo_test

import (
	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"net/http"
)

func newCredentialRefreshDriverWithSettings(httpClient *http.Client, settings map[string]string) *marketo.CredentialRefreshDriver {
	return marketo.NewCredentialRefreshDriver(httpClient, settings["tokenEndpoint"])
}
