// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

// CredentialRefreshDriver mints Marketo client-credentials access tokens and leaves persistence to Dex.
type CredentialRefreshDriver struct {
	httpClient    *http.Client
	tokenEndpoint string
	now           func() time.Time
}

// NewCredentialRefreshDriver binds the validated subscription token endpoint.
func NewCredentialRefreshDriver(httpClient *http.Client, tokenEndpoint string) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, tokenEndpoint: tokenEndpoint, now: time.Now}
}

// RefreshRequired reports when the token is absent or near expiry.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh exchanges the client credentials for a short-lived token.
func (driver *CredentialRefreshDriver) Refresh(ctx context.Context, state sdkgo.CredentialRefreshState[Credentials]) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil || driver.tokenEndpoint == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Marketo refresh driver is not configured")
	}
	credentials := state.Credentials
	endpoint := oauthtoken.TokenEndpoint{ProviderName: "Marketo", URL: driver.tokenEndpoint, HTTPClient: driver.httpClient, TerminalErrorCodes: []string{"invalid_client", "unauthorized_client", "invalid_scope"}}
	token, err := endpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: driver.now().UTC().Add(token.ExpiresIn)}, nil
}
