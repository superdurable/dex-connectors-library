// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector

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
	githubOAuthTokenEndpoint = "https://github.com/login/oauth/access_token"
	credentialRefreshSkew    = 5 * time.Minute
)

// CredentialRefreshDriver refreshes expiring GitHub OAuth credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

type githubTokenResponse struct {
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
// A credential without expiry metadata is treated as GitHub's supported non-expiring token form.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AccessToken.Reveal() == "" {
		return true
	}
	if state.ExpiresAt == nil {
		return false
	}
	return !state.ExpiresAt.After(state.Now.Add(credentialRefreshSkew))
}

// Refresh exchanges the stored rotating refresh token for a replacement token pair.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("GitHub credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.OAuthClientID == "" || credentials.OAuthClientSecret.Reveal() == "" || credentials.RefreshToken.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("GitHub OAuth refresh material is incomplete"))
	}
	token, err := driver.exchangeToken(ctx, url.Values{
		"client_id":     {credentials.OAuthClientID},
		"client_secret": {credentials.OAuthClientSecret.Reveal()},
		"grant_type":    {"refresh_token"},
		"refresh_token": {credentials.RefreshToken.Reveal()},
	})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !hasRequiredTokenScopes(token.Scope) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("GitHub credential lacks the required OAuth scopes"))
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
		return Credentials{}, errors.New("GitHub resolved credential is invalid")
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
	form url.Values,
) (githubTokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, githubOAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return githubTokenResponse{}, errors.New("GitHub token request could not be built")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := driver.httpClient.Do(request)
	if err != nil {
		return githubTokenResponse{}, fmt.Errorf("GitHub token endpoint is unavailable: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return githubTokenResponse{}, errors.New("GitHub token response could not be read")
	}
	var token githubTokenResponse
	if err := json.Unmarshal(contents, &token); err != nil {
		return githubTokenResponse{}, errors.New("GitHub token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || token.ErrorCode != "" {
		if token.ErrorCode == "bad_refresh_token" || token.ErrorCode == "incorrect_client_credentials" || token.ErrorCode == "invalid_client" || token.ErrorCode == "invalid_grant" {
			return githubTokenResponse{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("GitHub rejected credential refresh with %s", token.ErrorCode))
		}
		return githubTokenResponse{}, fmt.Errorf("GitHub token endpoint returned HTTP %d", response.StatusCode)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 || !strings.EqualFold(token.TokenType, "Bearer") {
		return githubTokenResponse{}, errors.New("GitHub token response omitted required fields")
	}
	return token, nil
}

func hasRequiredTokenScopes(value string) bool {
	scopes := map[string]bool{}
	for _, scope := range strings.FieldsFunc(value, func(character rune) bool {
		return character == ',' || character == ' '
	}) {
		if scope != "" {
			scopes[scope] = true
		}
	}
	return len(scopes) == 2 && scopes["read:user"] && scopes["user:email"]
}
