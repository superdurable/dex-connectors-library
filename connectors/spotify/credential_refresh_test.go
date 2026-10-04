// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify

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

func TestCredentialRefreshDriverRefreshesPKCEAndConfidentialClients(t *testing.T) {
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name         string
		clientSecret string
	}{
		{name: "PKCE client"},
		{name: "confidential client", clientSecret: "client-secret"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, spotifyOAuthTokenEndpoint, request.URL.String())
				require.Equal(t, "application/json", request.Header.Get("Accept"))
				require.NoError(t, request.ParseForm())
				require.Equal(t, "refresh_token", request.Form.Get("grant_type"))
				require.Equal(t, "existing-refresh", request.Form.Get("refresh_token"))
				if test.clientSecret == "" {
					require.Equal(t, "client-id", request.Form.Get("client_id"))
					_, _, hasBasicAuth := request.BasicAuth()
					require.False(t, hasBasicAuth)
				} else {
					require.Empty(t, request.Form.Get("client_id"))
					clientID, clientSecret, hasBasicAuth := request.BasicAuth()
					require.True(t, hasBasicAuth)
					require.Equal(t, "client-id", clientID)
					require.Equal(t, test.clientSecret, clientSecret)
				}
				return spotifyTokenResponseForTest(t, http.StatusOK, map[string]any{
					"access_token": "replacement-access", "expires_in": 3600, "refresh_token": "replacement-refresh",
					"scope": requiredOAuthScope, "token_type": "Bearer",
				}), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return now }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
				OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString(test.clientSecret),
				AccessToken: sdkgo.NewSecretString("existing-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
			}})
			require.NoError(t, err)
			require.Equal(t, "replacement-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, "replacement-refresh", result.Credentials.RefreshToken.Reveal())
			require.Equal(t, now.Add(time.Hour), result.ExpiresAt)
		})
	}
}

func TestCredentialRefreshDriverPreservesOmittedRefreshToken(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return spotifyTokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "replacement-access", "expires_in": 3600,
			"scope": requiredOAuthScope, "token_type": "Bearer",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", AccessToken: sdkgo.NewSecretString("existing-access"),
		RefreshToken: sdkgo.NewSecretString("existing-refresh"),
	}})
	require.NoError(t, err)
	require.Equal(t, "existing-refresh", result.Credentials.RefreshToken.Reveal())
}

func TestCredentialRefreshDriverClassifiesInvalidGrantWithoutProviderText(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return spotifyTokenResponseForTest(t, http.StatusBadRequest, map[string]any{
			"error": "invalid_grant", "error_description": "secret provider account detail",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "secret provider account detail")
}

func TestCredentialRefreshDriverRejectsMismatchedScope(t *testing.T) {
	client := &http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return spotifyTokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "replacement-access", "expires_in": 3600,
			"scope": requiredOAuthScope + " user-read-email", "token_type": "Bearer",
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{
		OAuthClientID: "client-id", RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "replacement-access")
}

func TestCredentialRefreshDriverUsesExpirySkew(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{})
	now := time.Date(2026, time.September, 29, 8, 0, 0, 0, time.UTC)
	credential := Credentials{AccessToken: sdkgo.NewSecretString("access-token")}
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, Now: now}))
	insideSkew := now.Add(credentialRefreshSkew)
	beyondSkew := insideSkew.Add(time.Nanosecond)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credential, ExpiresAt: &beyondSkew, Now: now}))
}

func spotifyTokenResponseForTest(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(contents))),
	}
}
