// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personio

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const personioTokenEndpoint = "https://api.personio.de/v2/auth/token"
const personioReadScope = "personio:persons:read"

// CredentialRefreshDriver exchanges Personio client credentials for a renewable token.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver uses the supplied transport or a bounded default client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired obtains a token when absent, expired, or lacking an expiry.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh requests a scoped token and leaves persistence to the credential provider.
func (driver *CredentialRefreshDriver) Refresh(ctx context.Context, state sdkgo.CredentialRefreshState[Credentials]) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Personio credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	credentials := state.Credentials
	endpoint := oauthtoken.TokenEndpoint{ProviderName: "Personio", URL: personioTokenEndpoint, HTTPClient: driver.httpClient, TerminalErrorCodes: []string{"invalid_client", "unauthorized_client", "invalid_scope"}}
	token, err := endpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost}, personioReadScope)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if token.Scope != "" && !hasPersonioReadScope(token.Scope) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Personio token lacks Persons read scope"))
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: driver.now().UTC().Add(token.ExpiresIn)}, nil
}

func hasPersonioReadScope(scope string) bool {
	for _, granted := range strings.Fields(scope) {
		if granted == personioReadScope {
			return true
		}
	}
	return false
}
