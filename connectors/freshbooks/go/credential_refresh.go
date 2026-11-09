// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshbooks

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
	freshbooksOAuthTokenEndpoint = "https://api.freshbooks.com/auth/oauth/token"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation request.
	credentialRefreshTimeout = 15 * time.Second
)

var (
	// freshbooksRequiredOAuthScopes must match the manifest scopes.
	freshbooksRequiredOAuthScopes = []string{"user:profile:read", "user:invoices:read"}
	// freshbooksTerminalRefreshErrorCodes are RFC 6749 section 5.2 codes; FreshBooks documents none of its own.
	freshbooksTerminalRefreshErrorCodes = []string{"invalid_client", "invalid_grant", "unauthorized_client"}
)

// CredentialRefreshDriver exchanges FreshBooks tokens while the provider owns locking and persistence.
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
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("FreshBooks credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:                "FreshBooks",
		URL:                         freshbooksOAuthTokenEndpoint,
		HTTPClient:                  driver.httpClient,
		TerminalErrorCodes:          freshbooksTerminalRefreshErrorCodes,
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
	if token.Scope != "" && (!oauthtoken.HasAllScopes(token.Scope, freshbooksRequiredOAuthScopes)) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("FreshBooks credential lacks the required invoice or profile scope"))
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
		return errors.New("FreshBooks access token is missing or malformed")
	}
	return nil
}

func refreshRequestParameters(redirectURI string) map[string]string {
	if redirectURI == "" {
		return nil
	}
	return map[string]string{"redirect_uri": redirectURI}
}
