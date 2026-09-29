// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	spotifyOAuthTokenEndpoint = "https://accounts.spotify.com/api/token"
	credentialRefreshSkew     = 5 * time.Minute
	requiredOAuthScope        = "playlist-read-private"
)

// CredentialRefreshDriver refreshes Spotify OAuth credentials before their one-hour access-token expiry.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

type spotifyTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ErrorCode    string `json:"error"`
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 15-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access credential is absent or expires within five minutes.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AccessToken.Reveal() == "" {
		return true
	}
	if state.ExpiresAt == nil {
		return false
	}
	return !state.ExpiresAt.After(state.Now.Add(credentialRefreshSkew))
}

// Refresh exchanges the stored Spotify refresh token and preserves it when Spotify omits a replacement.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Spotify credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.OAuthClientID == "" || credentials.RefreshToken.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Spotify OAuth refresh material is incomplete"))
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {credentials.RefreshToken.Reveal()},
	}
	token, err := driver.exchangeToken(ctx, credentials, form)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !hasExactOAuthScope(token.Scope) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Spotify credential does not match the required OAuth scope"))
	}
	credentials.AccessToken = sdkgo.NewSecretString(token.AccessToken)
	if token.RefreshToken != "" {
		credentials.RefreshToken = sdkgo.NewSecretString(token.RefreshToken)
	}
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second),
	}, nil
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped broker credential without renewal material.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AccessToken string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Spotify resolved credential is invalid")
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

func (driver *CredentialRefreshDriver) exchangeToken(
	ctx context.Context,
	credentials Credentials,
	form url.Values,
) (spotifyTokenResponse, error) {
	if credentials.OAuthClientSecret.Reveal() == "" {
		form.Set("client_id", credentials.OAuthClientID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, spotifyOAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return spotifyTokenResponse{}, errors.New("Spotify token request could not be built")
	}
	if credentials.OAuthClientSecret.Reveal() != "" {
		request.SetBasicAuth(credentials.OAuthClientID, credentials.OAuthClientSecret.Reveal())
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := driver.httpClient.Do(request)
	if err != nil {
		return spotifyTokenResponse{}, fmt.Errorf("Spotify token endpoint is unavailable: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return spotifyTokenResponse{}, errors.New("Spotify token response could not be read")
	}
	var token spotifyTokenResponse
	if err := json.Unmarshal(contents, &token); err != nil {
		return spotifyTokenResponse{}, errors.New("Spotify token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || token.ErrorCode != "" {
		if isTerminalRefreshError(token.ErrorCode) {
			return spotifyTokenResponse{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("Spotify rejected credential refresh with %s", safeOAuthErrorCode(token.ErrorCode)))
		}
		return spotifyTokenResponse{}, fmt.Errorf("Spotify token endpoint returned HTTP %d", response.StatusCode)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 || !strings.EqualFold(token.TokenType, "Bearer") {
		return spotifyTokenResponse{}, errors.New("Spotify token response omitted required fields")
	}
	return token, nil
}

func isTerminalRefreshError(errorCode string) bool {
	switch errorCode {
	case "invalid_grant", "invalid_client", "unauthorized_client", "access_denied":
		return true
	default:
		return false
	}
}

func safeOAuthErrorCode(errorCode string) string {
	if isTerminalRefreshError(errorCode) {
		return errorCode
	}
	return "unknown"
}

func hasExactOAuthScope(value string) bool {
	scopes := strings.Fields(value)
	return len(scopes) == 1 && scopes[0] == requiredOAuthScope
}
