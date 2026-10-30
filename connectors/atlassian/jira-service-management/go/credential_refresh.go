// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const atlassianOAuthTokenEndpoint = "https://auth.atlassian.com/oauth/token"

var (
	// requiredOAuthScopes are the classic scopes every refreshed credential must keep.
	requiredOAuthScopes = []string{
		"read:servicedesk-request", "write:servicedesk-request", "manage:servicedesk-customer", "read:jira-work", "write:jira-work",
	}
	// atlassianTerminalRefreshErrorCodes are the token endpoint errors that no retry can recover.
	atlassianTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}
)

// CredentialRefreshDriver refreshes Atlassian OAuth 2.0 (3LO) credentials for Jira Service Management.
// It performs the token exchange but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the Atlassian refresh driver.
// A nil HTTP client uses a 15-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded expiry, or expires within
// five minutes. Atlassian access tokens always expire, so a missing expiry refreshes once and records one.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored rotating refresh token for a replacement token pair. Atlassian disables the
// prior refresh token, so the result always carries the replacement. An invalid_grant response, which
// Atlassian sends with HTTP 403, and a grant that lost a required scope require reauthorization.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Jira Service Management credential refresh driver is not configured")
	}
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:            "Atlassian",
		URL:                     atlassianOAuthTokenEndpoint,
		HTTPClient:              driver.httpClient,
		TerminalErrorCodes:      atlassianTerminalRefreshErrorCodes,
		AcceptsMissingTokenType: true,
		UsesJSONRequestBody:     true,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if strings.TrimSpace(token.Scope) != "" && !oauthtoken.HasAllScopes(token.Scope, requiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Atlassian credential lacks a Jira Service Management or Jira scope; reconnect and accept every requested scope"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// validateResolvedCredentials accepts any credential that can authorize one request.
func validateResolvedCredentials(credentials Credentials) error {
	accessToken := credentials.AccessToken.Reveal()
	if accessToken == "" {
		return errors.New("Jira Service Management access token is required")
	}
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return errors.New("Jira Service Management access token is not a valid header value")
	}
	return nil
}
