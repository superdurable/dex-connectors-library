// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

// CredentialRefreshDriver mints Creatio Identity Service client-credentials tokens.
type CredentialRefreshDriver struct {
	httpClient    *http.Client
	tokenEndpoint string
	now           func() time.Time
}

// NewCredentialRefreshDriver uses a bounded client and the configured Identity Service endpoint.
func NewCredentialRefreshDriver(httpClient *http.Client, tokenEndpoint string) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, tokenEndpoint: tokenEndpoint, now: time.Now}
}

// RefreshRequired obtains a token when absent, expired, or lacking an expiry.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh requests a token and leaves persistence to the credential provider.
func (driver *CredentialRefreshDriver) Refresh(ctx context.Context, state sdkgo.CredentialRefreshState[Credentials]) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil || driver.tokenEndpoint == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Creatio credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	credentials := state.Credentials
	endpoint := oauthtoken.TokenEndpoint{ProviderName: "Creatio", URL: driver.tokenEndpoint, HTTPClient: driver.httpClient, TerminalErrorCodes: []string{"invalid_client", "unauthorized_client"}}
	token, err := endpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: driver.now().UTC().Add(token.ExpiresIn)}, nil
}
