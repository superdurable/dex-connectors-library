// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type credentialRoundTripFunc func(*http.Request) (*http.Response, error)

func (function credentialRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCredentialRefreshDriverRotatesBotAndUserCredentials(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	requests := 0
	client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, slackOAuthTokenEndpoint, request.URL.String())
		clientID, clientSecret, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "client-id", clientID)
		require.Equal(t, "client-secret", clientSecret)
		require.NoError(t, request.ParseForm())
		switch request.Form.Get("refresh_token") {
		case "bot-refresh":
			return slackTokenResponseForTest(t, map[string]any{
				"ok": true, "access_token": "new-bot", "expires_in": 43200,
				"refresh_token": "rotated-bot", "token_type": "bot", "scope": strings.Join(requiredBotScopes, ","),
			}), nil
		case "user-refresh":
			return slackTokenResponseForTest(t, map[string]any{
				"ok": true, "access_token": "new-user", "expires_in": 43200,
				"token_type": "user", "scope": strings.Join(requiredUserScopes, ","),
			}), nil
		default:
			t.Fatalf("unexpected refresh token")
			return nil, nil
		}
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		BotToken: sdkgo.NewSecretString("old-bot"), BotRefreshToken: sdkgo.NewSecretString("bot-refresh"),
		UserToken: sdkgo.NewSecretString("old-user"), UserRefreshToken: sdkgo.NewSecretString("user-refresh"),
		AppToken: sdkgo.NewSecretString("app-token"),
	}})
	require.NoError(t, err)
	require.Equal(t, 2, requests)
	require.Equal(t, "new-bot", result.Credentials.BotToken.Reveal())
	require.Equal(t, "rotated-bot", result.Credentials.BotRefreshToken.Reveal())
	require.Equal(t, "new-user", result.Credentials.UserToken.Reveal())
	require.Equal(t, "user-refresh", result.Credentials.UserRefreshToken.Reveal())
	require.Equal(t, "app-token", result.Credentials.AppToken.Reveal())
	require.Equal(t, now.Add(12*time.Hour), result.ExpiresAt)
}

func TestCredentialRefreshDriverClassifiesInvalidRefreshTokenWithoutProviderText(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return slackTokenResponseForTest(t, map[string]any{
			"ok": false, "error": "invalid_refresh_token", "response_metadata": map[string]any{"messages": []string{"secret workspace detail"}},
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		BotRefreshToken: sdkgo.NewSecretString("bot-refresh"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret workspace detail")
}

func TestCredentialRefreshDriverRejectsMismatchedScope(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return slackTokenResponseForTest(t, map[string]any{
			"ok": true, "access_token": "new-bot", "expires_in": 43200, "refresh_token": "rotated-bot",
			"token_type": "bot", "scope": strings.Join(append(append([]string{}, requiredBotScopes...), "admin"), ","),
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		BotRefreshToken: sdkgo.NewSecretString("bot-refresh"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "new-bot")
}

func TestCredentialRefreshDriverSupportsNonExpiringTokens(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{})
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	credentials := Credentials{
		BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"),
	}
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, Now: now}))
	insideSkew := now.Add(credentialRefreshSkew)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &insideSkew, Now: now}))
}

func slackTokenResponseForTest(t *testing.T, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(contents))),
	}
}
