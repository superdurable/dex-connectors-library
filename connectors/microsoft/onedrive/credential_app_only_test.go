// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func appOnlyCredentials(tenantID string) Credentials {
	return Credentials{
		AuthMethodID: MicrosoftAppOnlyAuthMethodID, TenantID: tenantID,
		AppClientID: "00000000-0000-0000-0000-0000000000bb", AppClientSecret: sdkgo.NewSecretString(testClientSecret),
	}
}

func TestAppOnlyRefreshRequestsGraphDefaultFromTheTenantEndpoint(t *testing.T) {
	for tenantID, wantURL := range map[string]string{
		"72f988bf-86f1-41af-91ab-2d7cd011db47": "https://login.microsoftonline.com/72f988bf-86f1-41af-91ab-2d7cd011db47/oauth2/v2.0/token",
		"Contoso.onmicrosoft.com":              "https://login.microsoftonline.com/contoso.onmicrosoft.com/oauth2/v2.0/token",
	} {
		requests := 0
		driver := newTestDriver(func(request *http.Request) (*http.Response, error) {
			requests++
			require.Equal(t, wantURL, request.URL.String())
			require.NoError(t, request.ParseForm())
			require.Equal(t, "client_credentials", request.Form.Get("grant_type"))
			require.Equal(t, "https://graph.microsoft.com/.default", request.Form.Get("scope"))
			require.Equal(t, "00000000-0000-0000-0000-0000000000bb", request.Form.Get("client_id"))
			require.Equal(t, testClientSecret, request.Form.Get("client_secret"))
			require.Empty(t, request.Header.Get("Authorization"))
			return tokenResponse(t, http.StatusOK, map[string]any{"access_token": "app-access", "token_type": "Bearer", "expires_in": 3599}), nil
		})
		result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: appOnlyCredentials(tenantID), Now: refreshTestNow})
		require.NoError(t, err)
		require.Equal(t, 1, requests)
		require.Equal(t, "app-access", result.Credentials.AccessToken.Reveal())
		require.Equal(t, testClientSecret, result.Credentials.AppClientSecret.Reveal())
		require.Empty(t, result.Credentials.RefreshToken.Reveal())
		require.Equal(t, refreshTestNow.Add(3599*time.Second), result.ExpiresAt)
	}
}

func TestAppOnlyRefreshValidatesTheTenantBeforeBuildingTheURL(t *testing.T) {
	driver := newTestDriver(func(*http.Request) (*http.Response, error) {
		t.Fatal("an invalid tenant must not reach the token endpoint")
		return nil, nil
	})
	for _, tenantID := range []string{"", "common", "organizations", "consumers", "contoso", "evil.example/../x", "a.b?x=1", "contoso.onmicrosoft.com#", "72f988bf-86f1-41af-91ab-2d7cd011db4"} {
		_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: appOnlyCredentials(tenantID), Now: refreshTestNow})
		require.True(t, sdkgo.IsReauthorizationRequired(err), tenantID)
	}
}

func TestAppOnlyRefreshTreatsARejectedClientAsTerminal(t *testing.T) {
	driver := newTestDriver(func(*http.Request) (*http.Response, error) {
		return tokenResponse(t, http.StatusUnauthorized, map[string]any{"error": "invalid_client", "error_description": "AADSTS7000215-SENTINEL"}), nil
	})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: appOnlyCredentials("contoso.onmicrosoft.com"), Now: refreshTestNow})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "SENTINEL")
}

func TestDecodeResolvedCredentialsJSONAcceptsOnlyTheMethodAndAccessToken(t *testing.T) {
	credentials, err := DecodeResolvedCredentialsJSON(json.RawMessage(`{"auth_method":"microsoft-app-only","access_token":"short-lived"}`))
	require.NoError(t, err)
	require.Equal(t, MicrosoftAppOnlyAuthMethodID, credentials.AuthMethodID)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	for _, contents := range []string{
		`{"auth_method":"microsoft-oauth","access_token":"a","refresh_token":"b"}`,
		`{"auth_method":"microsoft-app-only","access_token":"a","app_client_secret":"b"}`,
		`{"auth_method":"other","access_token":"a"}`,
		`{"access_token":"has space"}`, `{}`, `[]`,
	} {
		_, err := DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, contents)
		require.NotContains(t, err.Error(), "has space")
	}
}

func TestEncodeCredentialsJSONRoundTripsTheSelectedMethod(t *testing.T) {
	encoded, err := EncodeCredentialsJSON(appOnlyCredentials("contoso.onmicrosoft.com"))
	require.NoError(t, err)
	decoded, err := DecodeCredentialsJSON(encoded)
	require.NoError(t, err)
	require.Equal(t, "contoso.onmicrosoft.com", decoded.TenantID)
	require.Equal(t, testClientSecret, decoded.AppClientSecret.Reveal())
	_, err = EncodeCredentialsJSON(Credentials{AuthMethodID: MicrosoftOAuthAuthMethodID})
	require.Error(t, err)
}
