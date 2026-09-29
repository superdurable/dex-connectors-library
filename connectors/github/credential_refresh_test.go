// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package githubconnector

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

func TestCredentialRefreshDriverRefreshesOAuthAndHandlesRotation(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name            string
		rotatedRefresh  string
		expectedRefresh string
	}{
		{name: "preserves omitted refresh token", expectedRefresh: "existing-refresh"},
		{name: "persists rotated refresh token", rotatedRefresh: "rotated-refresh", expectedRefresh: "rotated-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, githubOAuthTokenEndpoint, request.URL.String())
				require.Equal(t, "application/json", request.Header.Get("Accept"))
				require.NoError(t, request.ParseForm())
				require.Equal(t, "client-id", request.Form.Get("client_id"))
				require.Equal(t, "client-secret", request.Form.Get("client_secret"))
				require.Equal(t, "existing-refresh", request.Form.Get("refresh_token"))
				response := map[string]any{
					"access_token": "new-access", "expires_in": 28800,
					"token_type": "bearer", "scope": "read:user,user:email",
				}
				if test.rotatedRefresh != "" {
					response["refresh_token"] = test.rotatedRefresh
				}
				return githubTokenResponseForTest(t, http.StatusOK, response), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return now }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{
				Credentials: Credentials{
					OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
					AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
				},
				Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, now.Add(8*time.Hour), result.ExpiresAt)
		})
	}
}

func TestCredentialRefreshDriverClassifiesBadRefreshTokenWithoutProviderText(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return githubTokenResponseForTest(t, http.StatusBadRequest, map[string]any{
			"error": "bad_refresh_token", "error_description": "secret provider account detail",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret provider account detail")
}

func TestCredentialRefreshDriverRejectsMismatchedScope(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return githubTokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "new-access", "expires_in": 28800, "token_type": "bearer",
			"scope": "read:user,user:email,repo",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "new-access")
}

func TestCredentialRefreshDriverSupportsNonExpiringTokens(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{})
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	credential := Credentials{AccessToken: sdkgo.NewSecretString("access-token")}
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, Now: now}))
	insideSkew := now.Add(credentialRefreshSkew)
	beyondSkew := insideSkew.Add(time.Nanosecond)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &beyondSkew, Now: now}))
}

func TestDecodeResolvedCredentialsRejectsRenewalMaterial(t *testing.T) {
	credentials, err := DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"short-lived"}`))
	require.NoError(t, err)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	_, err = DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"short-lived","refresh_token":"must-not-cross-broker"}`))
	require.Error(t, err)
}

func githubTokenResponseForTest(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(contents))),
	}
}
