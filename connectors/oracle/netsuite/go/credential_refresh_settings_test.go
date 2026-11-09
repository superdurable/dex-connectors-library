// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite_test

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go"
)

func newCredentialRefreshDriverWithSettings(httpClient *http.Client, settings map[string]string) *oraclenetsuite.CredentialRefreshDriver {
	return oraclenetsuite.NewCredentialRefreshDriver(httpClient, settings["tokenEndpoint"])
}
