// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package greenhouse

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const greenhouseTokenEndpoint = "https://auth.greenhouse.io/token"

// CredentialRefreshDriver exchanges Greenhouse custom-integration client credentials for a short-lived access token.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver uses the supplied HTTP transport, or a bounded default client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired obtains a token when absent or near expiry; a missing expiry requires renewal.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh requests a new token and leaves persistence to the credential provider.
func (driver *CredentialRefreshDriver) Refresh(ctx context.Context, state sdkgo.CredentialRefreshState[Credentials]) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Greenhouse credential refresh driver is not configured")
	}
	credentials := state.Credentials
	endpoint := oauthtoken.TokenEndpoint{ProviderName: "Greenhouse", URL: greenhouseTokenEndpoint, HTTPClient: driver.httpClient, TerminalErrorCodes: []string{"invalid_client", "unauthorized_client", "invalid_scope"}}
	token, err := endpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretBasic})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: driver.now().UTC().Add(token.ExpiresIn)}, nil
}
