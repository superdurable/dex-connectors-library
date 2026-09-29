// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

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
	slackOAuthTokenEndpoint = "https://slack.com/api/oauth.v2.access"
	credentialRefreshSkew   = 5 * time.Minute
)

var (
	requiredBotScopes  = []string{"channels:history", "groups:history", "channels:read", "groups:read", "users:read", "chat:write"}
	requiredUserScopes = []string{"channels:history", "groups:history"}
)

// CredentialRefreshDriver refreshes rotating Slack bot and user OAuth credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

type slackTokenResponse struct {
	OK           bool   `json:"ok"`
	ErrorCode    string `json:"error"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 25-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether rotating access credentials are absent or within five minutes of expiry.
// Credentials without expiry metadata retain Slack's supported non-expiring token behavior.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.BotToken.Reveal() == "" || state.Credentials.UserToken.Reveal() == "" {
		return true
	}
	if state.ExpiresAt == nil {
		return false
	}
	return !state.ExpiresAt.After(state.Now.Add(credentialRefreshSkew))
}

// Refresh rotates every configured Slack bot and user refresh token and returns one complete credential value.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Slack credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.OAuthClientID == "" || credentials.OAuthClientSecret.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Slack OAuth client credentials are incomplete"))
	}
	if credentials.BotRefreshToken.Reveal() == "" && credentials.UserRefreshToken.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Slack refresh tokens are unavailable"))
	}
	now := driver.now().UTC()
	expiresAt := time.Time{}
	if credentials.BotRefreshToken.Reveal() != "" {
		botToken, err := driver.exchangeToken(ctx, credentials, credentials.BotRefreshToken.Reveal(), "bot", requiredBotScopes)
		if err != nil {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		credentials.BotToken = sdkgo.NewSecretString(botToken.AccessToken)
		if botToken.RefreshToken != "" {
			credentials.BotRefreshToken = sdkgo.NewSecretString(botToken.RefreshToken)
		}
		expiresAt = now.Add(time.Duration(botToken.ExpiresIn) * time.Second)
	}
	if credentials.UserRefreshToken.Reveal() != "" {
		userToken, err := driver.exchangeToken(ctx, credentials, credentials.UserRefreshToken.Reveal(), "user", requiredUserScopes)
		if err != nil {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		credentials.UserToken = sdkgo.NewSecretString(userToken.AccessToken)
		if userToken.RefreshToken != "" {
			credentials.UserRefreshToken = sdkgo.NewSecretString(userToken.RefreshToken)
		}
		userExpiresAt := now.Add(time.Duration(userToken.ExpiresIn) * time.Second)
		if expiresAt.IsZero() || userExpiresAt.Before(expiresAt) {
			expiresAt = userExpiresAt
		}
	}
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: expiresAt}, nil
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes operation-scoped broker credentials without renewal material.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		BotToken  string `json:"bot_token"`
		UserToken string `json:"user_token"`
		AppToken  string `json:"app_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Slack resolved credential is invalid")
	}
	credentials := Credentials{
		BotToken: sdkgo.NewSecretString(fields.BotToken), UserToken: sdkgo.NewSecretString(fields.UserToken),
		AppToken: sdkgo.NewSecretString(fields.AppToken),
	}
	if fields.BotToken == "" && fields.UserToken == "" && fields.AppToken == "" {
		return Credentials{}, errors.New("Slack resolved credential is empty")
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

func (driver *CredentialRefreshDriver) exchangeToken(
	ctx context.Context,
	credentials Credentials,
	refreshToken string,
	expectedTokenType string,
	requiredScopes []string,
) (slackTokenResponse, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, slackOAuthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return slackTokenResponse{}, errors.New("Slack token request could not be built")
	}
	request.SetBasicAuth(credentials.OAuthClientID, credentials.OAuthClientSecret.Reveal())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := driver.httpClient.Do(request)
	if err != nil {
		return slackTokenResponse{}, fmt.Errorf("Slack token endpoint is unavailable: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return slackTokenResponse{}, errors.New("Slack token response could not be read")
	}
	var token slackTokenResponse
	if err := json.Unmarshal(contents, &token); err != nil {
		return slackTokenResponse{}, errors.New("Slack token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !token.OK {
		if isTerminalSlackRefreshError(token.ErrorCode) {
			return slackTokenResponse{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("Slack rejected credential refresh with %s", slackCode(token.ErrorCode)))
		}
		return slackTokenResponse{}, fmt.Errorf("Slack token endpoint returned HTTP %d with %s", response.StatusCode, slackCode(token.ErrorCode))
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 || token.TokenType != expectedTokenType {
		return slackTokenResponse{}, errors.New("Slack token response omitted required fields")
	}
	if !hasExactSlackScopes(token.Scope, requiredScopes) {
		return slackTokenResponse{}, sdkgo.NewReauthorizationRequiredError(errors.New("Slack credential does not match required scopes"))
	}
	return token, nil
}

func isTerminalSlackRefreshError(errorCode string) bool {
	switch errorCode {
	case "invalid_refresh_token", "bad_client_secret", "invalid_client_id", "invalid_auth", "token_revoked", "account_inactive", "access_denied":
		return true
	default:
		return false
	}
}

func hasExactSlackScopes(value string, required []string) bool {
	actual := map[string]bool{}
	for _, scope := range strings.Split(value, ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			actual[scope] = true
		}
	}
	if len(actual) != len(required) {
		return false
	}
	for _, scope := range required {
		if !actual[scope] {
			return false
		}
	}
	return true
}
