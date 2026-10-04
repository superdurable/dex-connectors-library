// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	githubOAuthTokenEndpoint = "https://github.com/login/oauth/access_token"
	credentialRefreshSkew    = oauthtoken.RefreshSkew
)

var (
	// githubRequiredOAuthScopes are the exact scopes a refreshed GitHub credential must carry.
	githubRequiredOAuthScopes = []string{"read:user", "user:email"}
	// githubTerminalRefreshErrorCodes are the GitHub token endpoint errors that no retry can recover.
	githubTerminalRefreshErrorCodes = []string{"bad_refresh_token", "incorrect_client_credentials", "invalid_client", "invalid_grant"}
)

// CredentialRefreshDriver refreshes expiring GitHub OAuth credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 15-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an expiring access credential is absent or within five minutes of expiry.
// A credential without expiry metadata is treated as GitHub's supported non-expiring token form.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.KeepWhenExpiryMissing,
	)
}

// Refresh exchanges the stored rotating refresh token for a replacement token pair.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("GitHub credential refresh driver is not configured")
	}
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "GitHub",
		URL:                githubOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: githubTerminalRefreshErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !oauthtoken.HasExactScopes(token.Scope, githubRequiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("GitHub credential lacks the required OAuth scopes"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
