// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	// pipedriveOAuthTokenEndpoint serves both the authorization code and the refresh token grants.
	pipedriveOAuthTokenEndpoint = "https://oauth.pipedrive.com/oauth/token"
	// apiDomainResponseField is the company API base URL that every Pipedrive token response carries.
	apiDomainResponseField = "api_domain"
)

var (
	// requiredOAuthScopes are the scopes every operation of this connector needs.
	requiredOAuthScopes = []string{"deals:full", "contacts:full"}
	// terminalRefreshErrorCodes are the token endpoint errors that no retry can recover.
	terminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

	errCredentialRefreshUnavailable = errors.New("Pipedrive OAuth token refresh is temporarily unavailable")
)

// CredentialRefreshDriver refreshes Pipedrive OAuth access tokens. It calls
// Pipedrive's token endpoint but leaves locking and atomic persistence to the
// credential provider. A Personal API token never needs a refresh.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 20-second client; the caller retains ownership of a
// supplied client. The token exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an OAuth connection has no access token, no
// recorded expiry, an expiry within five minutes, or no valid api_domain. It is
// always false for a Personal API token, which does not expire.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AuthMethodID != OAuthAuthMethodID {
		return false
	}
	if _, err := validatePipedriveBaseURL(state.Credentials.APIDomain); err != nil {
		return true
	}
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored refresh token for a new hourly access token at
// https://oauth.pipedrive.com/oauth/token, presenting the client with HTTP Basic
// authentication as Pipedrive recommends. Pipedrive returns the same refresh
// token with its 60-day window extended; the result carries it, or the prior one
// when Pipedrive omits it, and the api_domain the response names. A rejected
// grant or client, or a grant without the required scopes, returns an error
// wrapped by sdkgo.NewReauthorizationRequiredError; every other failure,
// including a response without a valid api_domain, is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Pipedrive credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Pipedrive Personal API tokens cannot be refreshed")
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:           "Pipedrive",
		URL:                    pipedriveOAuthTokenEndpoint,
		HTTPClient:             driver.httpClient,
		TerminalErrorCodes:     terminalRefreshErrorCodes,
		RetainedResponseFields: []string{apiDomainResponseField},
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretBasic,
	}, credentials.RefreshToken)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, requiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Pipedrive OAuth grant lacks deals:full or contacts:full"))
	}
	apiDomain, err := retainedAPIDomain(token.RetainedFields[apiDomainResponseField])
	if err != nil {
		// The grant itself was accepted, so the connection stays authorized and the next call refreshes again.
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	credentials.APIDomain = apiDomain
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// retainedAPIDomain validates the api_domain of a token response as a Pipedrive HTTPS origin.
func retainedAPIDomain(rawAPIDomain json.RawMessage) (string, error) {
	var apiDomain string
	if len(rawAPIDomain) == 0 || json.Unmarshal(rawAPIDomain, &apiDomain) != nil {
		return "", errors.New("Pipedrive token response has no api_domain")
	}
	baseURL, err := validatePipedriveBaseURL(apiDomain)
	if err != nil {
		return "", errors.New("Pipedrive token response has an invalid api_domain")
	}
	return baseURL, nil
}
