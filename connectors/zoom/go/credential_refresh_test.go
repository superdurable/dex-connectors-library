// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const grantedZoomScopes = "meeting:read:list_meetings meeting:read:meeting meeting:write:meeting meeting:update:meeting meeting:read:list_past_participants user:read:user"

type tokenRoundTripFunc func(*http.Request) (*http.Response, error)

func (function tokenRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCredentialRefreshUsesBasicClientAuthenticationAndKeepsTheRotatedRefreshToken(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name            string
		rotatedRefresh  string
		expectedRefresh string
	}{
		{name: "persists the rotated refresh token", rotatedRefresh: "rotated-refresh", expectedRefresh: "rotated-refresh"},
		{name: "keeps the prior token when Zoom omits one", expectedRefresh: "existing-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: tokenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, zoomOAuthTokenEndpoint, request.URL.String())
				require.Equal(t, http.MethodPost, request.Method)
				clientID, clientSecret, hasBasicAuthentication := request.BasicAuth()
				require.True(t, hasBasicAuthentication, "Zoom documents HTTP Basic client authentication")
				require.Equal(t, "client-id", clientID)
				require.Equal(t, "client-secret", clientSecret)
				require.NoError(t, request.ParseForm())
				require.Equal(t, "refresh_token", request.PostForm.Get("grant_type"))
				require.Equal(t, "existing-refresh", request.PostForm.Get("refresh_token"))
				require.Empty(t, request.PostForm.Get("client_secret"), "the secret travels only in the Basic header")
				response := map[string]any{
					"access_token": "new-access", "token_type": "bearer", "expires_in": 3599,
					"scope": grantedZoomScopes, "api_url": "https://api.zoom.us",
				}
				if test.rotatedRefresh != "" {
					response["refresh_token"] = test.rotatedRefresh
				}
				return tokenResponseForTest(t, http.StatusOK, response), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return now }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{
				Credentials: refreshableCredentials(), Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, "client-id", result.Credentials.OAuthClientID)
			require.Equal(t, now.Add(3599*time.Second), result.ExpiresAt)
		})
	}
}

func TestCredentialRefreshRequiresReauthorizationForRejectedGrantsAndMissingScopes(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		response map[string]any
	}{
		{name: "expired refresh token", status: http.StatusBadRequest, response: map[string]any{"error": "invalid_grant", "reason": "SENTINEL account detail"}},
		{name: "used refresh token", status: http.StatusBadRequest, response: map[string]any{"error": "invalid_request", "reason": "SENTINEL Invalid Token!"}},
		{name: "rotated client secret", status: http.StatusUnauthorized, response: map[string]any{"error": "invalid_client", "reason": "SENTINEL"}},
		{name: "scope removed from the app", status: http.StatusOK, response: map[string]any{
			"access_token": "new-access", "token_type": "bearer", "expires_in": 3599, "refresh_token": "next", "scope": "meeting:read:meeting",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return tokenResponseForTest(t, test.status, test.response), nil
			})}
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: refreshableCredentials()})
			require.Error(t, err)
			require.True(t, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "SENTINEL")
			require.NotContains(t, err.Error(), "new-access")
		})
	}
}

func TestCredentialRefreshRetriesWhenZoomIsUnavailable(t *testing.T) {
	client := &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusServiceUnavailable, map[string]any{"error": "invalid_grant"}), nil
	})}
	_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: refreshableCredentials()})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "a 5xx is retryable even with a terminal code")
}

func TestCredentialRefreshIsRequiredForMissingOrExpiringTokens(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("access")}
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, Now: now}), "Zoom issues only expiring tokens")
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Now: now}))
	insideSkew, beyondSkew := now.Add(5*time.Minute), now.Add(5*time.Minute+time.Second)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &beyondSkew, Now: now}))
}

func refreshableCredentials() Credentials {
	return Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
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
