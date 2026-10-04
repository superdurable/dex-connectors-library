// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import "net/http"

// NewLocalProviderHTTPClientForTest exposes the loopback transport to the external test package.
func NewLocalProviderHTTPClientForTest(baseURL string) (*http.Client, error) {
	return newLocalProviderHTTPClient(nil, baseURL)
}
