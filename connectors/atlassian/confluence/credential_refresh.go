// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"bytes"
	"context"
	"encoding/json"
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
	// confluenceRequiredOAuthScopes are the scopes every refreshed credential must keep; offline_access is implied.
	confluenceRequiredOAuthScopes = []string{
		"search:confluence", "read:page:confluence", "write:page:confluence",
		"read:space:confluence", "read:comment:confluence", "write:comment:confluence",
	}
	// atlassianTerminalRefreshErrorCodes are the token endpoint errors that no retry can recover.
	atlassianTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}
)

// CredentialRefreshDriver refreshes Atlassian OAuth 2.0 (3LO) credentials for Confluence.
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
// Atlassian sends with HTTP 403, and a grant that lost a Confluence scope require reauthorization.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Confluence credential refresh driver is not configured")
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
	if strings.TrimSpace(token.Scope) != "" && !oauthtoken.HasAllScopes(token.Scope, confluenceRequiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Atlassian credential lacks a requested Confluence scope; reconnect and accept every requested scope"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped broker credential. Renewal material is
// intentionally absent from this short-lived representation and is rejected.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AccessToken string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Confluence resolved credential is invalid")
	}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString(fields.AccessToken)}
	return credentials, validateResolvedCredentials(credentials)
}

// EncodeCredentialsJSON encodes complete credential material for trusted atomic persistence.
func EncodeCredentialsJSON(credentials Credentials) ([]byte, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return encodeLocalCredentials(credentials)
}

// validateResolvedCredentials accepts any credential that can authorize one request.
func validateResolvedCredentials(credentials Credentials) error {
	accessToken := credentials.AccessToken.Reveal()
	if accessToken == "" {
		return errors.New("Confluence access token is required")
	}
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return errors.New("Confluence access token is not a valid header value")
	}
	return nil
}
