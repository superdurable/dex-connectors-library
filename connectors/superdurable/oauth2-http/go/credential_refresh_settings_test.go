// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http"
)

// newCredentialRefreshDriverWithSettings builds the driver from the token endpoint and client authentication
// settings that connector.yaml's declarations read. The generated agreement test's token endpoint lives in its
// transport, which the address policy cannot dial, so the driver sends its requests there directly.
func newCredentialRefreshDriverWithSettings(httpClient *http.Client, settings map[string]string) *oauth2http.CredentialRefreshDriver {
	driver, err := oauth2http.NewCredentialRefreshDriverThroughTransportForTest(oauth2http.Config{
		TokenEndpoint:           settings["tokenEndpoint"],
		TokenEndpointAuthMethod: oauth2http.TokenEndpointAuthMethod(settings["tokenEndpointAuthMethod"]),
	}, httpClient.Transport)
	if err != nil {
		panic("the agreement test's settings must build a credential refresh driver: " + err.Error())
	}
	return driver
}
