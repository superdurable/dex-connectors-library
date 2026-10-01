// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

// mondayOAuthTokenEndpoint is the OAuth 2.1 token endpoint; the legacy /oauth2/token issues non-expiring tokens.
const mondayOAuthTokenEndpoint = "https://auth.monday.com/oauth_ms/oauth/token"

// nominalAccessTokenLifetime applies without a readable exp; monday.com's migrate endpoint documents 3600 seconds.
const nominalAccessTokenLifetime = time.Hour

const maxAccessTokenBytes = 16 << 10

var (
	// requiredOAuthScopes are the scopes every operation of this connector needs.
	requiredOAuthScopes = []string{"boards:read", "boards:write", "updates:write"}
	// terminalRefreshErrorCodes are the token endpoint errors that no retry can recover.
	terminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client", "invalid_token"}

	errCredentialRefreshUnavailable = errors.New("monday.com OAuth token refresh is temporarily unavailable")
)

// CredentialRefreshDriver refreshes monday.com OAuth 2.1 access tokens. It calls monday.com's
// token endpoint but leaves locking and atomic persistence to the credential provider. A
// personal API token never needs a refresh.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 12-second client; the caller retains ownership of a supplied
// client. The token exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an OAuth access token is absent or expires within five
// minutes. monday.com returns no expires_in, so a token without a recorded expiry uses the
// exp claim inside its JWT, and refreshes when that claim is unreadable. It is always false
// for a personal API token, which does not expire.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AuthMethodID != OAuthAuthMethodID {
		return false
	}
	expiresAt := state.ExpiresAt
	if expiresAt == nil {
		if expiry, isKnown := readAccessTokenExpiry(state.Credentials.AccessToken); isKnown {
			expiresAt = &expiry
		}
	}
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", expiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored refresh token for a new access token and refresh token.
// monday.com rotates the refresh token on every refresh, so the result carries the returned
// one, or the prior one if monday.com ever omits it. A rejected grant, or a grant without the
// required scopes, returns an error wrapped by sdkgo.NewReauthorizationRequiredError; every
// other failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("monday.com credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("monday.com personal API tokens cannot be refreshed")
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:            "monday.com",
		URL:                     mondayOAuthTokenEndpoint,
		HTTPClient:              driver.httpClient,
		TerminalErrorCodes:      terminalRefreshErrorCodes,
		AcceptsMissingExpiresIn: true,
		UsesJSONRequestBody:     true,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, requiredOAuthScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("monday.com OAuth grant lacks a required scope"))
	}
	now := driver.now().UTC()
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: refreshedTokenExpiry(token, now)}, nil
}

// refreshedTokenExpiry prefers expires_in, then the JWT exp claim, and always returns a future time.
func refreshedTokenExpiry(token oauthtoken.TokenResponse, now time.Time) time.Time {
	if token.ExpiresIn > 0 {
		return now.Add(token.ExpiresIn)
	}
	if expiry, isKnown := readAccessTokenExpiry(token.AccessToken); isKnown && expiry.After(now) {
		return expiry
	}
	return now.Add(nominalAccessTokenLifetime)
}

// readAccessTokenExpiry reads the exp claim of a JWT access token without verifying it; it only schedules refresh.
func readAccessTokenExpiry(accessToken sdkgo.SecretString) (time.Time, bool) {
	value := accessToken.Reveal()
	if value == "" || len(value) > maxAccessTokenBytes {
		return time.Time{}, false
	}
	segments := strings.Split(value, ".")
	if len(segments) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		ExpiresAt *float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.ExpiresAt == nil || *claims.ExpiresAt <= 0 || *claims.ExpiresAt > math.MaxInt32*4 {
		return time.Time{}, false
	}
	return time.Unix(int64(*claims.ExpiresAt), 0).UTC(), true
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation,
// including the selected auth_method.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes the operation-scoped credential a trusted hosted broker
// returns, for use as a hostedconfig.CredentialDecoder: auth_method with api_token for a personal
// API token, or with access_token for OAuth. Renewal material and unknown fields are rejected,
// and the error never repeats a value. A blank auth_method means a personal API token.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		APIToken     string `json:"api_token"`
		AccessToken  string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("monday.com resolved credential is invalid")
	}
	credentials := Credentials{AuthMethodID: fields.AuthMethodID, APIToken: sdkgo.NewSecretString(fields.APIToken), AccessToken: sdkgo.NewSecretString(fields.AccessToken)}
	if credentials.AuthMethodID == "" {
		credentials.AuthMethodID = PersonalAPITokenAuthMethodID
	}
	switch {
	case credentials.AuthMethodID == PersonalAPITokenAuthMethodID && fields.AccessToken != "",
		credentials.AuthMethodID == OAuthAuthMethodID && fields.APIToken != "":
		return Credentials{}, errors.New("monday.com resolved credential carries the other method's token")
	case validateResolvedCredentials(credentials) != nil:
		return Credentials{}, errors.New("monday.com resolved credential is invalid")
	}
	return credentials, nil
}

// EncodeCredentialsJSON encodes complete credential material for trusted atomic persistence.
func EncodeCredentialsJSON(credentials Credentials) ([]byte, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return encodeLocalCredentials(credentials)
}
