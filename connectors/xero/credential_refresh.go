// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	xeroTokenEndpoint = "https://" + identityHost + "/connect/token"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation request.
	credentialRefreshTimeout = 8 * time.Second
)

var (
	// accountingScopes must match the manifest; the OAuth method also requests offline_access.
	accountingScopes = []string{"accounting.invoices", "accounting.payments", "accounting.contacts.read"}
	// terminalTokenErrorCodes are the token endpoint errors that no retry can recover; Xero documents
	// invalid_client, invalid_grant, unauthorized_client, and unsupported_grant_type.
	terminalTokenErrorCodes = []string{"invalid_client", "invalid_grant", "unauthorized_client", "unsupported_grant_type", "invalid_scope"}

	errCredentialRefreshUnavailable = errors.New("Xero access token request is temporarily unavailable")
)

// CredentialRefreshDriver obtains Xero access tokens. For a Custom Connection it repeats the
// client credentials grant; for OAuth it exchanges the rotating refresh token. Both present the
// client as HTTP Basic credentials, as Xero documents. It calls Xero's token endpoint but
// leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses an 8-second client; the caller retains ownership of a supplied
// client. The token exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: credentialRefreshTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded expiry, or
// expires within five minutes. Xero access tokens last 30 minutes for both methods.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected method. A Custom Connection
// requests exactly the connector's accounting scopes. An OAuth refresh stores the refresh
// token Xero rotates on every refresh, or keeps the prior one if Xero ever omits it. A
// rejected client, grant, or scope, or a returned scope list without a required scope,
// returns an error wrapped by sdkgo.NewReauthorizationRequiredError; every other failure
// is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Xero credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Xero",
		URL:                xeroTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: terminalTokenErrorCodes,
	}
	client := oauthtoken.ClientCredentials{
		ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretBasic,
	}
	var token oauthtoken.TokenResponse
	var err error
	switch credentials.AuthMethodID {
	case CustomConnectionAuthMethodID:
		token, err = tokenEndpoint.ExchangeClientCredentials(ctx, client, accountingScopes...)
	case OAuthAuthMethodID:
		token, err = tokenEndpoint.ExchangeRefreshToken(ctx, client, credentials.RefreshToken)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Xero authorization method is not supported"))
	}
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, accountingScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Xero granted a token without a required accounting scope"))
	}
	credentials.AccessToken = token.AccessToken
	if credentials.AuthMethodID == OAuthAuthMethodID {
		credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	}
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// validateResolvedCredentials checks only what a request needs, so credentials without renewal material pass.
func validateResolvedCredentials(credentials Credentials) error {
	switch credentials.AuthMethodID {
	case CustomConnectionAuthMethodID, OAuthAuthMethodID:
	default:
		return errors.New("Xero auth_method is invalid")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Xero access token is missing or is not printable ASCII without spaces")
	}
	return nil
}
