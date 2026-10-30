// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

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

const grantedTeamsScopes = "Channel.ReadBasic.All ChannelMessage.Read.All ChannelMessage.Send Chat.ReadBasic ChatMessage.Send Team.ReadBasic.All"

type credentialRoundTripFunc func(*http.Request) (*http.Response, error)

func (function credentialRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCredentialRefreshSendsAFormGrantAndKeepsTheReplacementRefreshToken(t *testing.T) {
	now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, microsoftOAuthTokenEndpoint, request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		contents, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(contents))
		require.NoError(t, err)
		require.Equal(t, url.Values{
			"grant_type": {"refresh_token"}, "client_id": {"client-id"}, "client_secret": {"client-secret"}, "refresh_token": {"refresh-1"},
		}, form, "no scope parameter, so the token keeps every granted permission")
		// Microsoft does not echo offline_access, and the driver never requires it.
		return tokenResponseForTest(t, http.StatusOK, map[string]any{
			"token_type": "Bearer", "access_token": "access-2", "refresh_token": "refresh-2", "expires_in": 4271, "ext_expires_in": 4271, "scope": grantedTeamsScopes,
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }

	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials(), Now: now})
	require.NoError(t, err)
	require.Equal(t, "access-2", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "refresh-2", result.Credentials.RefreshToken.Reveal())
	require.Equal(t, "client-id", result.Credentials.OAuthClientID)
	require.Equal(t, now.Add(4271*time.Second), result.ExpiresAt)
}

func TestCredentialRefreshKeepsThePriorRefreshTokenWhenNoneIsReturned(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusOK, map[string]any{"token_type": "Bearer", "access_token": "access-2", "expires_in": 3599}), nil
	})})
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials()})
	require.NoError(t, err)
	require.Equal(t, "refresh-1", result.Credentials.RefreshToken.Reveal())
}

func TestCredentialRefreshTreatsSignInErrorsAsReauthorizationWithoutProviderText(t *testing.T) {
	for _, code := range []string{"invalid_grant", "interaction_required", "invalid_client", "consent_required"} {
		t.Run(code, func(t *testing.T) {
			driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return tokenResponseForTest(t, http.StatusBadRequest, map[string]any{
					"error": code, "error_description": "AADSTS70008: SENTINEL The provided authorization code or refresh token has expired.", "error_codes": []int{70008},
				}), nil
			})})
			_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials()})
			require.Error(t, err)
			require.True(t, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "SENTINEL")
			require.NotContains(t, err.Error(), "refresh-1")
		})
	}
}

func TestCredentialRefreshRetriesTemporaryFailures(t *testing.T) {
	for name, response := range map[string]func(*testing.T) *http.Response{
		"outage with a terminal code": func(t *testing.T) *http.Response {
			return tokenResponseForTest(t, http.StatusServiceUnavailable, map[string]any{"error": "invalid_grant"})
		},
		"temporarily unavailable": func(t *testing.T) *http.Response {
			return tokenResponseForTest(t, http.StatusBadRequest, map[string]any{"error": "temporarily_unavailable"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(t), nil
			})})
			_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials()})
			require.Error(t, err)
			require.False(t, sdkgo.IsReauthorizationRequired(err))
		})
	}
}

func TestCredentialRefreshIsRequiredWithoutARecordedExpiry(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	credentials := testRefreshCredentials()
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, Now: now}))
	later := now.Add(time.Hour)
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}))
	soon := now.Add(time.Minute)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &soon, Now: now}))
}

func testRefreshCredentials() Credentials {
	return Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("access-1"), RefreshToken: sdkgo.NewSecretString("refresh-1"),
	}
}

func tokenResponseForTest(t *testing.T, status int, body map[string]any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(bytes.NewReader(encoded)),
	}
}
