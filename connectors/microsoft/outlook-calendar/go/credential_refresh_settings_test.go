// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
)

// newCredentialRefreshDriverWithSettings passes the app-only tenantId setting, which the driver takes in its
// constructor, to the generated credential refresh agreement test.
func newCredentialRefreshDriverWithSettings(httpClient *http.Client, settings map[string]string) *outlookcalendar.CredentialRefreshDriver {
	return outlookcalendar.NewCredentialRefreshDriver(httpClient, settings["tenantId"])
}
