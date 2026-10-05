// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

// docusignTerminalRefreshErrorCodes are RFC 6749 section 5.2 codes; DocuSign documents none of its own.
var docusignTerminalRefreshErrorCodes = []string{"invalid_request", "invalid_client", "invalid_grant", "unauthorized_client"}

// CredentialRefreshDriver refreshes DocuSign access tokens, which expire after eight hours. The
// driver performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the refresh driver. A nil HTTP client uses a 25-second client;
// the caller keeps ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	return newCredentialRefreshDriver(httpClient, time.Now)
}

// newCredentialRefreshDriver lets New share the client's clock with the driver.
func newCredentialRefreshDriver(httpClient *http.Client, now func() time.Time) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: docusignRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: now}
}

// RefreshRequired reports whether the access token is absent, has no recorded expiry, or expires
// within five minutes.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored refresh token at the token endpoint of the connection's environment,
// sending the client as HTTP Basic credentials as DocuSign documents. With the extended scope DocuSign
// returns a new refresh token with a full lifetime, which replaces the old one in the result; an
// omitted one keeps the prior token. A rejected grant requires reauthorization.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("DocuSign credential refresh driver is not configured")
	}
	credentials := state.Credentials
	environment, isKnown := docusignEnvironmentFor(credentials.AuthMethodID)
	if !isKnown {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errDocuSignAuthMethodUnknown)
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "DocuSign",
		URL:                environment.accountServerURL + "/oauth/token",
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: docusignTerminalRefreshErrorCodes,
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
