// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const calendlyOAuthTokenEndpoint = "https://auth.calendly.com/oauth/token"

// calendlyTerminalRefreshErrorCodes need reauthorization; a reused single-use refresh token is invalid_grant.
var calendlyTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

// CredentialRefreshDriver refreshes Calendly OAuth access tokens, which expire after two hours. A personal
// access token never needs a refresh. The driver performs provider calls but leaves locking and atomic
// persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the refresh driver. A nil HTTP client uses a 25-second client; the
// caller keeps ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: calendlyRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an OAuth access token is absent, has no recorded expiry, or expires
// within five minutes. It is always false for a personal access token.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AuthMethodID == PersonalAccessTokenAuthMethodID {
		return false
	}
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored single-use refresh token, sending the client as HTTP Basic credentials as
// Calendly documents for web apps. The rotated refresh token replaces the old one in the result, so the
// provider persists it before the next refresh. A rejected grant requires reauthorization.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Calendly credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.AuthMethodID == PersonalAccessTokenAuthMethodID {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("a Calendly personal access token cannot be refreshed; generate a new token"))
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Calendly",
		URL:                calendlyOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: calendlyTerminalRefreshErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretBasic,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
