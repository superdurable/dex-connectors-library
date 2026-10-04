// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	linkedInOAuthTokenEndpoint = "https://www.linkedin.com/oauth/v2/accessToken"
	credentialRefreshSkew      = oauthtoken.RefreshSkew
)

var requiredOAuthScopes = []string{"openid", "profile", "email"}

// linkedInTerminalRefreshErrorCodes are the LinkedIn token endpoint errors that no retry can recover.
var linkedInTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client", "access_denied"}

// CredentialRefreshDriver refreshes LinkedIn credentials when the authorized product supplies a refresh token.
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
// A credential without expiry metadata remains usable until LinkedIn rejects it or interactive reauthorization occurs.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.KeepWhenExpiryMissing,
	)
}

// Refresh exchanges an approved programmatic refresh token for replacement LinkedIn credentials.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("LinkedIn credential refresh driver is not configured")
	}
	credentials := state.Credentials
	// LinkedIn issues refresh tokens only to approved products, so name that cause instead of a generic one.
	if credentials.OAuthClientID == "" || credentials.OAuthClientSecret.Reveal() == "" || credentials.RefreshToken.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("LinkedIn programmatic refresh material is unavailable"))
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:            "LinkedIn",
		URL:                     linkedInOAuthTokenEndpoint,
		HTTPClient:              driver.httpClient,
		TerminalErrorCodes:      linkedInTerminalRefreshErrorCodes,
		AcceptsMissingTokenType: true,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !oauthtoken.HasExactScopes(token.Scope, requiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("LinkedIn credential does not match the required OpenID Connect scopes"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
