// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio_test

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
)

func newCredentialRefreshDriverWithSettings(httpClient *http.Client, settings map[string]string) *creatio.CredentialRefreshDriver {
	return creatio.NewCredentialRefreshDriver(httpClient, settings["tokenEndpoint"])
}
