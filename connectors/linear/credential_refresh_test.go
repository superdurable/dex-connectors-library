// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type credentialRoundTripFunc func(*http.Request) (*http.Response, error)

func (function credentialRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func oauthCredentialsForTest() Credentials {
	return Credentials{
		AuthMethodID: OAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
		WebhookSigningSecret: sdkgo.NewSecretString("signing-secret"),
	}
}

func tokenResponseForTest(t *testing.T, status int, body map[string]any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(encoded))}
}

func TestCredentialRefreshDriverExchangesTheRotatingRefreshTokenAsAForm(t *testing.T) {
	now := time.Date(2026, time.October, 4, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "https://api.linear.app/oauth/token", request.URL.String())
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"), "Linear requires a form body")
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, url.Values{
			"grant_type": {"refresh_token"}, "refresh_token": {"existing-refresh"}, "client_id": {"client-id"}, "client_secret": {"client-secret"},
		}, form)
		return tokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "new-access", "refresh_token": "rotated-refresh", "token_type": "Bearer", "expires_in": 86399, "scope": "read write",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentialsForTest(), Now: now})
	require.NoError(t, err)
	require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "rotated-refresh", result.Credentials.RefreshToken.Reveal())
	require.Equal(t, "signing-secret", result.Credentials.WebhookSigningSecret.Reveal(), "the webhook secret survives a refresh")
	require.Equal(t, now.Add(86399*time.Second), result.ExpiresAt, "a future expiry from expires_in")
}

func TestCredentialRefreshDriverKeepsTheRefreshTokenWhenLinearOmitsIt(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusOK, map[string]any{"access_token": "new-access", "token_type": "Bearer", "expires_in": 86399}), nil
	})}
	result, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentialsForTest(), Now: time.Now()})
	require.NoError(t, err)
	require.Equal(t, "existing-refresh", result.Credentials.RefreshToken.Reveal())
}

func TestCredentialRefreshDriverSeparatesRevokedGrantsFromOutages(t *testing.T) {
	for name, testCase := range map[string]struct {
		status                    int
		body                      map[string]any
		isReauthorizationRequired bool
	}{
		"invalid grant":   {status: 400, body: map[string]any{"error": "invalid_grant"}, isReauthorizationRequired: true},
		"invalid client":  {status: 400, body: map[string]any{"error": "invalid_client", "error_description": "Invalid client: client is invalid"}, isReauthorizationRequired: true},
		"lost write":      {status: 200, body: map[string]any{"access_token": "new", "token_type": "Bearer", "expires_in": 86399, "scope": "read"}, isReauthorizationRequired: true},
		"outage":          {status: 503, body: map[string]any{"error": "invalid_grant"}},
		"array scope app": {status: 200, body: map[string]any{"access_token": "new", "token_type": "Bearer", "expires_in": 86399, "scope": []any{"read", "write"}}},
	} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return tokenResponseForTest(t, testCase.status, testCase.body), nil
			})}
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentialsForTest(), Now: time.Now()})
			require.Error(t, err)
			require.Equal(t, testCase.isReauthorizationRequired, sdkgo.IsReauthorizationRequired(err), err.Error())
			require.NotContains(t, err.Error(), "client-secret")
			require.NotContains(t, err.Error(), "existing-refresh")
		})
	}
}

func TestCredentialRefreshDriverNeverRefreshesAPersonalAPIKey(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("a personal API key never reaches the token endpoint")
		return nil, nil
	})})
	apiKey := Credentials{AuthMethodID: PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString("key")}
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: apiKey, Now: time.Now()}))
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: apiKey, Now: time.Now()})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

func TestCredentialRefreshDriverRefreshesFiveMinutesBeforeExpiry(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.October, 4, 8, 0, 0, 0, time.UTC)
	soon, later := now.Add(4*time.Minute), now.Add(time.Hour)
	credentials := oauthCredentialsForTest()
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &soon, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, Now: now}), "a missing expiry refreshes")
}
