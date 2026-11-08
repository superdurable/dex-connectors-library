// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todo

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const microsoftTokenEndpoint = "https://login.microsoftonline.com/organizations/oauth2/v2.0/token"

// CredentialRefreshDriver exchanges the saved Microsoft refresh token and leaves persistence to Dex.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the OAuth refresh dependency with an eight-second request limit.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent or nearing its expiry.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh obtains a rotated access token and refresh token for the consented Tasks.Read scope.
func (driver *CredentialRefreshDriver) Refresh(ctx context.Context, state sdkgo.CredentialRefreshState[Credentials]) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Microsoft To Do refresh driver is not configured")
	}
	endpoint := oauthtoken.TokenEndpoint{
		ProviderName: "Microsoft", URL: microsoftTokenEndpoint, HTTPClient: driver.httpClient,
		TerminalErrorCodes:          []string{"invalid_grant", "invalid_client", "unauthorized_client", "invalid_request", "invalid_scope", "interaction_required", "consent_required"},
		AdditionalRequestParameters: map[string]string{"scope": "Tasks.Read offline_access"},
	}
	credentials := state.Credentials
	token, err := endpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID: credentials.OAuthClientID, Secret: credentials.OAuthClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if token.Scope != "" {
		granted := false
		for _, scope := range strings.Fields(token.Scope) {
			if strings.EqualFold(strings.TrimPrefix(scope, "https://graph.microsoft.com/"), "Tasks.Read") {
				granted = true
			}
		}
		if !granted {
			return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Microsoft To Do Tasks.Read consent is missing"))
		}
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: driver.now().UTC().Add(token.ExpiresIn)}, nil
}
