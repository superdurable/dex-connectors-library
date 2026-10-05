// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const linearOAuthTokenEndpoint = "https://api.linear.app/oauth/token"

var (
	// linearTerminalRefreshErrorCodes need reauthorization; a revoked or reused refresh token is invalid_grant.
	linearTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}
	// requiredOAuthScopes is what every write needs; Linear always grants read alongside it.
	requiredOAuthScopes = []string{"write"}

	errCredentialRefreshUnavailable = errors.New("Linear OAuth token refresh is temporarily unavailable")
)

// CredentialRefreshDriver refreshes Linear OAuth access tokens, which expire after 24 hours; every refresh
// rotates the refresh token. A personal API key never needs a refresh. The driver performs provider calls
// but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the refresh driver. A nil HTTP client uses a 12-second client; the
// caller keeps ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an OAuth access token is absent, has no recorded expiry, or expires
// within five minutes. It is always false for a personal API key.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AuthMethodID == PersonalAPIKeyAuthMethodID {
		return false
	}
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored refresh token with the client ID and secret in the form body. The rotated
// refresh token replaces the old one in the result, so the provider persists it before the next refresh;
// Linear accepts the old token again for 30 minutes if that answer is lost. A rejected grant, or a grant
// that no longer includes write, requires reauthorization.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Linear credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.AuthMethodID == PersonalAPIKeyAuthMethodID {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("a Linear personal API key cannot be refreshed; create a new key"))
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Linear",
		URL:                linearOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: linearTerminalRefreshErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	case err != nil:
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.Join(errCredentialRefreshUnavailable, err)
	case token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, requiredOAuthScopes):
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("the Linear grant no longer includes the write scope; authorize the connection again"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
