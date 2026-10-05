// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	zohoOAuthTokenPath = "/oauth/v2/token"
	apiDomainField     = "api_domain"
	// zohoAccessTokenLifetime is the one-hour lifetime Zoho documents; a longer expires_in is never trusted.
	zohoAccessTokenLifetime = time.Hour
	// refreshRequestTimeout leaves room for a resend after a refresh inside operationDeadline.
	refreshRequestTimeout = 8 * time.Second
)

// zohoTerminalRefreshErrorCodes are the token endpoint errors Zoho documents for a refresh no retry can recover:
// a revoked or invalid refresh token, an unknown client or a data center that does not hold it, and a wrong secret.
var zohoTerminalRefreshErrorCodes = []string{"invalid_code", "invalid_client", "invalid_client_secret"}

// CredentialRefreshDriver refreshes Zoho OAuth access tokens at the Zoho Accounts server of the
// connection's data center and records the api_domain Zoho returns with them. It performs the
// token exchange but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the Zoho refresh driver. It uses a copy of httpClient that
// never follows redirects and, when httpClient sets no Timeout or is nil, times out after 8
// seconds. The caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	return &CredentialRefreshDriver{httpClient: providerhttp.NewProviderHTTPClient(httpClient, refreshRequestTimeout), now: time.Now}
}

// RefreshRequired reports whether the access token or the api_domain is absent, the access token
// has no recorded expiry, or it expires within oauthtoken.RefreshSkew. Zoho access tokens always
// expire after an hour, so a missing expiry refreshes once and records one.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	hasSession := state.Credentials.AccessToken.Reveal() != "" && state.Credentials.APIDomain != ""
	return oauthtoken.IsRefreshRequired(hasSession, state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing)
}

// Refresh exchanges the stored refresh token at POST {accounts server}/oauth/v2/token of the
// method's data center, with a form body and the client secret in the body, and stores the
// returned api_domain after checking that it belongs to the data center. Zoho does not rotate
// refresh tokens, so the prior one is kept. Zoho reports grant errors with HTTP 200 and an error
// code; invalid_code, invalid_client, and invalid_client_secret, and an unknown data center,
// return an error wrapped by sdkgo.NewReauthorizationRequiredError. A response without api_domain
// keeps the stored one, and an api_domain of another data center is retried, never stored.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Zoho credential refresh driver is not configured")
	}
	credentials := state.Credentials
	dataCenter, isKnown := DataCenterForAuthMethod(credentials.AuthMethodID)
	if !isKnown {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Zoho data center of the connection is not supported"))
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName: "Zoho", URL: dataCenter.AccountsURL + zohoOAuthTokenPath, HTTPClient: driver.httpClient,
		TerminalErrorCodes: zohoTerminalRefreshErrorCodes, RetainedResponseFields: []string{apiDomainField},
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID: credentials.OAuthClientID, Secret: credentials.OAuthClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	apiDomain, err := refreshedAPIDomain(token, credentials.APIDomain, dataCenter)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	credentials.APIDomain = apiDomain
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(min(token.ExpiresIn, zohoAccessTokenLifetime)),
	}, nil
}

// refreshedAPIDomain prefers the returned api_domain, falls back to the stored one, and finally to production.
func refreshedAPIDomain(token oauthtoken.TokenResponse, storedAPIDomain string, dataCenter DataCenter) (string, error) {
	rawAPIDomain, isReturned := token.RetainedFields[apiDomainField]
	if !isReturned {
		if storedAPIDomain != "" {
			return dataCenter.ValidateAPIDomain(storedAPIDomain)
		}
		return dataCenter.ProductionAPIDomain(), nil
	}
	var apiDomain string
	if err := json.Unmarshal(rawAPIDomain, &apiDomain); err != nil {
		return "", errors.New("Zoho token response api_domain is not a string")
	}
	return dataCenter.ValidateAPIDomain(apiDomain)
}
