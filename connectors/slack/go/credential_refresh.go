// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	slackOAuthTokenEndpoint = "https://slack.com/api/oauth.v2.access"
	credentialRefreshSkew   = oauthtoken.RefreshSkew
)

var (
	requiredBotScopes  = []string{"channels:history", "groups:history", "channels:read", "groups:read", "users:read", "chat:write"}
	requiredUserScopes = []string{"channels:history", "groups:history"}
	// slackTerminalRefreshErrorCodes are the Slack oauth.v2.access errors that no retry can recover.
	slackTerminalRefreshErrorCodes = []string{
		"invalid_refresh_token", "bad_client_secret", "invalid_client_id", "invalid_auth", "token_revoked", "account_inactive", "access_denied",
	}
)

// CredentialRefreshDriver refreshes rotating Slack bot and user OAuth credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
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
	hasAccessTokens := state.Credentials.BotToken.Reveal() != "" && state.Credentials.UserToken.Reveal() != ""
	return oauthtoken.IsRefreshRequired(hasAccessTokens, state.ExpiresAt, state.Now, oauthtoken.KeepWhenExpiryMissing)
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
		botToken, err := driver.exchangeRefreshToken(ctx, credentials, credentials.BotRefreshToken, "bot", requiredBotScopes)
		if err != nil {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		credentials.BotToken = botToken.AccessToken
		credentials.BotRefreshToken = botToken.NextRefreshToken(credentials.BotRefreshToken)
		expiresAt = now.Add(botToken.ExpiresIn)
	}
	if credentials.UserRefreshToken.Reveal() != "" {
		userToken, err := driver.exchangeRefreshToken(ctx, credentials, credentials.UserRefreshToken, "user", requiredUserScopes)
		if err != nil {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		credentials.UserToken = userToken.AccessToken
		credentials.UserRefreshToken = userToken.NextRefreshToken(credentials.UserRefreshToken)
		userExpiresAt := now.Add(userToken.ExpiresIn)
		if expiresAt.IsZero() || userExpiresAt.Before(expiresAt) {
			expiresAt = userExpiresAt
		}
	}
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: credentials, ExpiresAt: expiresAt}, nil
}

// exchangeRefreshToken rotates one Slack refresh token. Slack reports failures as ok:false with an error
// code, which the token endpoint treats as a failure like any other error code.
func (driver *CredentialRefreshDriver) exchangeRefreshToken(
	ctx context.Context,
	credentials Credentials,
	refreshToken sdkgo.SecretString,
	expectedTokenType string,
	requiredScopes []string,
) (oauthtoken.TokenResponse, error) {
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Slack",
		URL:                slackOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: slackTerminalRefreshErrorCodes,
		AcceptedTokenTypes: []string{expectedTokenType},
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretBasic,
	}, refreshToken)
	if err != nil {
		return oauthtoken.TokenResponse{}, err
	}
	if !oauthtoken.HasExactScopes(token.Scope, requiredScopes) {
		return oauthtoken.TokenResponse{}, sdkgo.NewReauthorizationRequiredError(errors.New("Slack credential does not match required scopes"))
	}
	return token, nil
}
