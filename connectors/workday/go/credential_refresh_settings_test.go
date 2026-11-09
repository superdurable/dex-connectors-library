// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workday_test

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/connectors/workday/go"
)

func newCredentialRefreshDriverWithSettings(httpClient *http.Client, settings map[string]string) *workday.CredentialRefreshDriver {
	return workday.NewCredentialRefreshDriver(httpClient, settings["tokenEndpoint"])
}
