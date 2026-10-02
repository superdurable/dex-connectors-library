// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ConnectionInternalsForTest exposes a Connection's client and reference to the external test package.
func ConnectionInternalsForTest(connection Connection) (*Client, sdkgo.ConnectionRef) {
	return connection.client, connection.reference
}

// NewLocalProviderHTTPClientForTest exposes the loopback transport to the external test package.
func NewLocalProviderHTTPClientForTest(baseURL string) (*http.Client, error) {
	return newLocalProviderHTTPClient(nil, baseURL)
}
