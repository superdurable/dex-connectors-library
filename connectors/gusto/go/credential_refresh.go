// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gusto

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	gustoOAuthTokenEndpoint = "https://api.gusto.com/oauth/token"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation request.
	credentialRefreshTimeout = 15 * time.Second
)

var (
	// gustoRequiredOAuthScopes must match the manifest scopes.
	gustoRequiredOAuthScopes = []string{"employees:read"}
	// gustoTerminalRefreshErrorCodes are RFC 6749 section 5.2 codes; Gusto documents none of its own.
	gustoTerminalRefreshErrorCodes = []string{"invalid_client", "invalid_grant", "unauthorized_client"}
)

// CredentialRefreshDriver exchanges Gusto tokens while the provider owns locking and persistence.
type CredentialRefreshDriver struct {
	httpClient  *http.Client
	redirectURI string
	now         func() time.Time
}

// NewCredentialRefreshDriver uses a 15-second client; live refresh requires the registered redirect URI.
func NewCredentialRefreshDriver(httpClient *http.Client, redirectURI ...string) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: credentialRefreshTimeout}
	}
	driver := &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
	if len(redirectURI) > 0 {
		driver.redirectURI = redirectURI[0]
	}
	return driver
}

// RefreshRequired checks whether a token is absent or near expiry.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges a token with JSON credentials and preserves any omitted replacement refresh token.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Gusto credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:                "Gusto",
		URL:                         gustoOAuthTokenEndpoint,
		HTTPClient:                  driver.httpClient,
		TerminalErrorCodes:          gustoTerminalRefreshErrorCodes,
		UsesJSONRequestBody:         true,
		AdditionalRequestParameters: refreshRequestParameters(driver.redirectURI),
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if token.Scope != "" && (!oauthtoken.HasAllScopes(token.Scope, gustoRequiredOAuthScopes)) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Gusto credential lacks the required employee scope"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// validateResolvedCredentials requires an access token that can travel in a bearer header.
func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Gusto access token is missing or malformed")
	}
	return nil
}

func refreshRequestParameters(redirectURI string) map[string]string {
	if redirectURI == "" {
		return nil
	}
	return map[string]string{"redirect_uri": redirectURI}
}
