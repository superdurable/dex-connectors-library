// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personio

import "net/http"

func newCredentialRefreshDriverWithSettings(httpClient *http.Client, _ map[string]string) *CredentialRefreshDriver {
	return NewCredentialRefreshDriver(httpClient)
}
