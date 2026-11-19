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

	// PublicAuthMethodID is the default, recommended authorization method. It requests read:user and
	// user:email, so GitHub serves only public repository data, and createIssue selects insufficientScope.
	PublicAuthMethodID = "github-oauth"
	// RepositoryAuthMethodID requests the repo scope that createIssue requires. The profile, public
	// repository, change, and release queries select insufficientScope on a connection that uses it.
	RepositoryAuthMethodID = "github-oauth-repository"
)

var (
	// githubPublicOAuthScopes are the exact scopes a PublicAuthMethodID credential carries.
	githubPublicOAuthScopes = []string{"read:user", "user:email"}
	// githubRepositoryOAuthScopes are the exact scopes a RepositoryAuthMethodID credential carries.
	githubRepositoryOAuthScopes = []string{"repo"}
	// githubTerminalRefreshErrorCodes are the GitHub token endpoint errors that no retry can recover.
	githubTerminalRefreshErrorCodes = []string{"bad_refresh_token", "incorrect_client_credentials", "invalid_client", "invalid_grant"}
)

// requiredOAuthScopes returns the exact scopes GitHub grants the method; a blank method is the manifest default.
func requiredOAuthScopes(authMethodID string) ([]string, bool) {
	switch authMethodID {
	case "", PublicAuthMethodID:
		return githubPublicOAuthScopes, true
	case RepositoryAuthMethodID:
		return githubRepositoryOAuthScopes, true
	}
	return nil, false
}

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
	requiredScopes, isKnownMethod := requiredOAuthScopes(credentials.AuthMethodID)
	if !isKnownMethod {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("GitHub authorization method is not supported"))
	}
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
	if !oauthtoken.HasExactScopes(token.Scope, requiredScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("GitHub credential lacks the required OAuth scopes"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
