// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

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
	constantContactOAuthTokenEndpoint = "https://authz.constantcontact.com/oauth2/default/v1/token"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation request.
	credentialRefreshTimeout = 15 * time.Second
)

var (
	// constantContactRequiredOAuthScopes must match the manifest scopes.
	constantContactRequiredOAuthScopes = []string{"contact_data"}
	// constantContactTerminalRefreshErrorCodes are RFC 6749 section 5.2 codes; Constant Contact documents none of its own.
	constantContactTerminalRefreshErrorCodes = []string{"invalid_client", "invalid_grant", "unauthorized_client"}
)

// CredentialRefreshDriver exchanges Constant Contact tokens while the provider owns locking and persistence.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver uses a 15-second client when none is supplied.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: credentialRefreshTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired checks whether a token is absent or near expiry.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges a token with HTTP Basic app credentials and preserves an omitted replacement refresh token.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Constant Contact credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Constant Contact",
		URL:                constantContactOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: constantContactTerminalRefreshErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretBasic,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, constantContactRequiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Constant Contact credential lacks the contact_data scope"))
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
		return errors.New("Constant Contact access token is missing or malformed")
	}
	return nil
}
