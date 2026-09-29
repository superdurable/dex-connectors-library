// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedinconnector

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
	linkedInOAuthTokenEndpoint = "https://www.linkedin.com/oauth/v2/accessToken"
	credentialRefreshSkew      = 5 * time.Minute
)

var requiredOAuthScopes = []string{"openid", "profile", "email"}

// CredentialRefreshDriver refreshes LinkedIn credentials when the authorized product supplies a refresh token.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

type linkedInTokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
	TokenType             string `json:"token_type"`
	ErrorCode             string `json:"error"`
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 15-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an expiring access credential is absent or within five minutes of expiry.
// A credential without expiry metadata remains usable until LinkedIn rejects it or interactive reauthorization occurs.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AccessToken.Reveal() == "" {
		return true
	}
	if state.ExpiresAt == nil {
		return false
	}
	return !state.ExpiresAt.After(state.Now.Add(credentialRefreshSkew))
}

// Refresh exchanges an approved programmatic refresh token for replacement LinkedIn credentials.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("LinkedIn credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.OAuthClientID == "" || credentials.OAuthClientSecret.Reveal() == "" || credentials.RefreshToken.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("LinkedIn programmatic refresh material is unavailable"))
	}
	token, err := driver.exchangeToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {credentials.RefreshToken.Reveal()},
		"client_id":     {credentials.OAuthClientID},
		"client_secret": {credentials.OAuthClientSecret.Reveal()},
	})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !hasExactOAuthScopes(token.Scope) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("LinkedIn credential does not match the required OpenID Connect scopes"))
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
		return Credentials{}, errors.New("LinkedIn resolved credential is invalid")
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

func (driver *CredentialRefreshDriver) exchangeToken(ctx context.Context, form url.Values) (linkedInTokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, linkedInOAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return linkedInTokenResponse{}, errors.New("LinkedIn token request could not be built")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := driver.httpClient.Do(request)
	if err != nil {
		return linkedInTokenResponse{}, fmt.Errorf("LinkedIn token endpoint is unavailable: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return linkedInTokenResponse{}, errors.New("LinkedIn token response could not be read")
	}
	var token linkedInTokenResponse
	if err := json.Unmarshal(contents, &token); err != nil {
		return linkedInTokenResponse{}, errors.New("LinkedIn token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || token.ErrorCode != "" {
		if isTerminalRefreshError(token.ErrorCode) {
			return linkedInTokenResponse{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("LinkedIn rejected credential refresh with %s", safeOAuthErrorCode(token.ErrorCode)))
		}
		return linkedInTokenResponse{}, fmt.Errorf("LinkedIn token endpoint returned HTTP %d", response.StatusCode)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 || (token.TokenType != "" && !strings.EqualFold(token.TokenType, "Bearer")) {
		return linkedInTokenResponse{}, errors.New("LinkedIn token response omitted required fields")
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
	switch errorCode {
	case "invalid_grant", "invalid_client", "unauthorized_client", "access_denied":
		return errorCode
	default:
		return "unknown"
	}
}

func hasExactOAuthScopes(value string) bool {
	actual := map[string]bool{}
	for _, scope := range strings.Fields(value) {
		actual[scope] = true
	}
	if len(actual) != len(requiredOAuthScopes) {
		return false
	}
	for _, scope := range requiredOAuthScopes {
		if !actual[scope] {
			return false
		}
	}
	return true
}
