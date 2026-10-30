// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func newRefreshDriver(t *testing.T, handler func(response http.ResponseWriter, request recordedRequest)) (*excel.CredentialRefreshDriver, *recordingServer) {
	t.Helper()
	server := newRecordingServer(t, handler)
	transport, err := excel.NewLocalProviderHTTPClientForTest(server.URL)
	require.NoError(t, err)
	return excel.NewCredentialRefreshDriver(transport), server
}

func oauthState(refreshToken string) sdkgo.CredentialRefreshState[excel.Credentials] {
	return sdkgo.CredentialRefreshState[excel.Credentials]{Credentials: excel.Credentials{
		AuthMethodID: excel.OAuthAuthMethodID, OAuthClientID: "00001111-aaaa-2222-bbbb-3333cccc4444",
		OAuthClientSecret: sdkgo.NewSecretString("client-secret-SENTINEL"), AccessToken: sdkgo.NewSecretString("old-access"),
		RefreshToken: sdkgo.NewSecretString(refreshToken),
	}, Now: time.Now()}
}

func TestRefreshExchangesTheRotatingRefreshTokenAtTheOrganizationsEndpoint(t *testing.T) {
	driver, server := newRefreshDriver(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"Bearer","expires_in":3599,"ext_expires_in":3599,
			"scope":"https://graph.microsoft.com/Files.ReadWrite.All https://graph.microsoft.com/User.Read","access_token":"new-access","refresh_token":"new-refresh"}`)
	})
	result, err := driver.Refresh(context.Background(), oauthState("old-refresh"))
	require.NoError(t, err)
	require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "new-refresh", result.Credentials.RefreshToken.Reveal())
	require.WithinDuration(t, time.Now().Add(3599*time.Second), result.ExpiresAt, 5*time.Second)

	request := server.recorded()[0]
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/organizations/oauth2/v2.0/token", request.path)
	form, err := url.ParseQuery(string(request.body))
	require.NoError(t, err)
	require.Equal(t, "refresh_token", form.Get("grant_type"))
	require.Equal(t, "old-refresh", form.Get("refresh_token"))
	require.Equal(t, "00001111-aaaa-2222-bbbb-3333cccc4444", form.Get("client_id"))
	require.Equal(t, "client-secret-SENTINEL", form.Get("client_secret"))
	require.Equal(t, "offline_access Files.ReadWrite.All", form.Get("scope"))
}

func TestRefreshKeepsThePriorRefreshTokenAndAcceptsShortOrMissingScopes(t *testing.T) {
	for name, scope := range map[string]string{"short form": `"scope":"files.readwrite.all User.Read",`, "omitted": ""} {
		driver, _ := newRefreshDriver(t, func(response http.ResponseWriter, _ recordedRequest) {
			writeJSON(t, response, http.StatusOK, `{"token_type":"Bearer","expires_in":3599,`+scope+`"access_token":"new-access"}`)
		})
		result, err := driver.Refresh(context.Background(), oauthState("kept-refresh"))
		require.NoError(t, err, name)
		require.Equal(t, "kept-refresh", result.Credentials.RefreshToken.Reveal(), name)
	}
}

func TestRefreshRequiresReauthorizationForTerminalErrorsAndMissingPermissions(t *testing.T) {
	for name, scenario := range map[string]struct {
		status int
		body   string
	}{
		"revoked grant":      {http.StatusBadRequest, `{"error":"invalid_grant","error_description":"AADSTS70008 SENTINEL"}`},
		"interaction needed": {http.StatusBadRequest, `{"error":"interaction_required"}`},
		"missing permission": {http.StatusOK, `{"token_type":"Bearer","expires_in":3599,"scope":"User.Read","access_token":"a"}`},
	} {
		driver, _ := newRefreshDriver(t, func(response http.ResponseWriter, _ recordedRequest) {
			writeJSON(t, response, scenario.status, scenario.body)
		})
		_, err := driver.Refresh(context.Background(), oauthState("old-refresh"))
		require.True(t, sdkgo.IsReauthorizationRequired(err), name)
		require.NotContains(t, err.Error(), "SENTINEL", name)
	}
	driver, _ := newRefreshDriver(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`)
	})
	_, err := driver.Refresh(context.Background(), oauthState("old-refresh"))
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "an outage is retryable")
}
