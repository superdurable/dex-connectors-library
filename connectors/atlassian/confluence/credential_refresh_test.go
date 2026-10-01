// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

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

const grantedConfluenceScopes = "search:confluence read:page:confluence write:page:confluence read:space:confluence read:comment:confluence write:comment:confluence offline_access"

type credentialRoundTripFunc func(*http.Request) (*http.Response, error)

func (function credentialRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCredentialRefreshSendsAJSONGrantAndPersistsTheRotatedRefreshToken(t *testing.T) {
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: credentialRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, atlassianOAuthTokenEndpoint, request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.Contains(t, request.Header.Get("Content-Type"), "application/json")
		var body map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]string{
			"grant_type": "refresh_token", "client_id": "client-id", "client_secret": "client-secret", "refresh_token": "refresh-1",
		}, body)
		return tokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "access-2", "refresh_token": "refresh-2", "expires_in": 3600, "scope": grantedConfluenceScopes,
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }

	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials(), Now: now})
	require.NoError(t, err)
	require.Equal(t, "access-2", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "refresh-2", result.Credentials.RefreshToken.Reveal(), "Atlassian disables the prior refresh token")
	require.Equal(t, "client-id", result.Credentials.OAuthClientID)
	require.Equal(t, now.Add(time.Hour), result.ExpiresAt)
}

func TestCredentialRefreshTreatsInvalidGrantAsReauthorizationWithoutProviderText(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusForbidden, map[string]any{"error": "invalid_grant", "error_description": "SENTINEL Unknown or invalid refresh token."}), nil
	})})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials()})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "SENTINEL")
	require.NotContains(t, err.Error(), "refresh-1")
}

func TestCredentialRefreshRetriesAnOutageEvenWithATerminalCode(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusServiceUnavailable, map[string]any{"error": "invalid_grant"}), nil
	})})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials()})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err))
}

func TestCredentialRefreshRequiresEveryConfluenceScope(t *testing.T) {
	driver := NewCredentialRefreshDriver(&http.Client{Transport: credentialRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "access-2", "refresh_token": "refresh-2", "expires_in": 3600,
			"scope": "search:confluence read:page:confluence read:space:confluence offline_access",
		}), nil
	})})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: testRefreshCredentials()})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

func TestCredentialRefreshIsRequiredWithoutARecordedExpiry(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	credentials := testRefreshCredentials()
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, Now: now}))
	later := now.Add(time.Hour)
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}))
	soon := now.Add(time.Minute)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &soon, Now: now}))
}

func TestHostedCredentialDecodingAcceptsOnlyTheAccessToken(t *testing.T) {
	credentials, err := DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"access-1"}`))
	require.NoError(t, err)
	require.Equal(t, "access-1", credentials.AccessToken.Reveal())
	_, err = DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"access-1","refresh_token":"refresh-1"}`))
	require.Error(t, err)
	_, err = DecodeResolvedCredentialsJSON(json.RawMessage(`{"access_token":"line\nbreak"}`))
	require.Error(t, err)

	encoded, err := EncodeCredentialsJSON(testRefreshCredentials())
	require.NoError(t, err)
	decoded, err := DecodeCredentialsJSON(encoded)
	require.NoError(t, err)
	require.Equal(t, "refresh-1", decoded.RefreshToken.Reveal())
	_, err = EncodeCredentialsJSON(Credentials{AccessToken: sdkgo.NewSecretString("access-1")})
	require.Error(t, err)
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
