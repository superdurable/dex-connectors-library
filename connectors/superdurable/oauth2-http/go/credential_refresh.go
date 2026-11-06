// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

var (
	// terminalRefreshErrorCodes need reauthorization; a revoked, expired, or already used refresh token is invalid_grant.
	terminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

	errCredentialRefreshUnavailable = errors.New("OAuth 2.0 token refresh is temporarily unavailable")
)

// CredentialRefreshDriver refreshes OAuth access tokens at the token endpoint that the connection's
// settings name. Every refresh sends the refresh token and replaces it with the one the server returns,
// so the server may invalidate a refresh token the moment it is used. The driver performs provider calls
// but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	tokenEndpointURL string
	authMethod       oauthtoken.ClientAuthenticationMethod
	httpClient       *http.Client
	now              func() time.Time
}

// NewCredentialRefreshDriver constructs the refresh driver for config's token endpoint and client
// authentication method. A nil HTTP client uses a client with the connection's requestTimeout; the caller
// keeps ownership of a supplied client, whose transport must be nil or an *http.Transport. The driver
// connects as WithHTTPClient describes. It fails when the token endpoint is not an absolute http or https
// URL without user information.
func NewCredentialRefreshDriver(config Config, httpClient *http.Client) (*CredentialRefreshDriver, error) {
	config = withConfigDefaults(config)
	if _, err := parseHTTPURL(config.TokenEndpoint); err != nil {
		return nil, fmt.Errorf("OAuth 2.0 HTTP tokenEndpoint: %w", err)
	}
	httpClient, err := newPrivateNetworkHTTPClient(httpClient, config.RequestTimeout)
	if err != nil {
		return nil, err
	}
	authMethod := oauthtoken.ClientSecretPost
	if config.TokenEndpointAuthMethod == TokenEndpointAuthMethodClientSecretBasic {
		authMethod = oauthtoken.ClientSecretBasic
	}
	return &CredentialRefreshDriver{
		tokenEndpointURL: config.TokenEndpoint, authMethod: authMethod, httpClient: httpClient, now: time.Now,
	}, nil
}

// RefreshRequired reports whether the access token is absent, or expires within the SDK's refresh skew,
// or has no recorded expiry while a refresh token can renew it. A token without a recorded expiry and
// without a refresh token is kept: the server issued a long-lived token that nothing can renew.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	hasAccessToken := state.Credentials.AccessToken.Reveal() != ""
	if hasAccessToken && state.ExpiresAt == nil && state.Credentials.RefreshToken.Reveal() == "" {
		return false
	}
	return oauthtoken.IsRefreshRequired(hasAccessToken, state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh exchanges the stored refresh token with the client ID and secret for a new access token. A
// rotated refresh token replaces the old one in the result, which the provider persists before the next
// refresh; when the server omits it, the old one stays. A rejected grant, such as an expired or already
// used refresh token, or a missing refresh token requires reauthorization. Any other failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("OAuth 2.0 HTTP credential refresh driver is not configured")
	}
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       providerName,
		URL:                driver.tokenEndpointURL,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: terminalRefreshErrorCodes,
		AllowsPlainHTTP:    true,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: driver.authMethod,
	}, credentials.RefreshToken)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	case err != nil:
		// A refused address stays in the chain, so the operation reports a defect instead of retrying.
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.Join(errCredentialRefreshUnavailable, err)
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
