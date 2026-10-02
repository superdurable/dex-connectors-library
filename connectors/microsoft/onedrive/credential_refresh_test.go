// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

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

// testClientSecret is deliberately not shaped like an Entra client secret.
const testClientSecret = "client-secret-value"

var refreshTestNow = time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func tokenResponse(t *testing.T, status int, body map[string]any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(contents)))}
}

func newTestDriver(handler roundTripFunc) *CredentialRefreshDriver {
	driver := NewCredentialRefreshDriver(&http.Client{Transport: handler})
	driver.now = func() time.Time { return refreshTestNow }
	return driver
}

func delegatedCredentials() Credentials {
	return Credentials{
		AuthMethodID: MicrosoftOAuthAuthMethodID, OAuthClientID: "00000000-0000-0000-0000-0000000000aa",
		OAuthClientSecret: sdkgo.NewSecretString(testClientSecret), AccessToken: sdkgo.NewSecretString("old-access"),
		RefreshToken: sdkgo.NewSecretString("existing-refresh"),
	}
}

func TestDelegatedRefreshUsesTheOrganizationsEndpointAndKeepsTheRotatedToken(t *testing.T) {
	for _, test := range []struct{ name, rotated, wantRefresh string }{
		{name: "rotated refresh token", rotated: "rotated-refresh", wantRefresh: "rotated-refresh"},
		{name: "omitted refresh token", wantRefresh: "existing-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := newTestDriver(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, "https://login.microsoftonline.com/organizations/oauth2/v2.0/token", request.URL.String())
				require.NoError(t, request.ParseForm())
				require.Equal(t, "refresh_token", request.Form.Get("grant_type"))
				require.Equal(t, "existing-refresh", request.Form.Get("refresh_token"))
				require.Equal(t, "00000000-0000-0000-0000-0000000000aa", request.Form.Get("client_id"))
				require.Equal(t, testClientSecret, request.Form.Get("client_secret"))
				require.Equal(t, "Files.ReadWrite.All Sites.Read.All offline_access", request.Form.Get("scope"))
				body := map[string]any{"access_token": "new-access", "token_type": "Bearer", "expires_in": 3599, "scope": "Files.ReadWrite.All Sites.Read.All"}
				if test.rotated != "" {
					body["refresh_token"] = test.rotated
				}
				return tokenResponse(t, http.StatusOK, body), nil
			})
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: delegatedCredentials(), Now: refreshTestNow})
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.wantRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, refreshTestNow.Add(3599*time.Second), result.ExpiresAt)
		})
	}
}

// Microsoft never echoes offline_access, so the check requires only what operations need.
func TestDelegatedRefreshChecksOnlyFilesReadWriteAll(t *testing.T) {
	for scope, isAccepted := range map[string]bool{
		"":                                   true,
		"Files.ReadWrite.All":                true,
		"files.readwrite.all profile openid": true,
		"https://graph.microsoft.com/Files.ReadWrite.All https://graph.microsoft.com/Sites.Read.All": true,
		"Sites.Read.All offline_access": false,
		"Files.Read.All":                false,
	} {
		driver := newTestDriver(func(*http.Request) (*http.Response, error) {
			return tokenResponse(t, http.StatusOK, map[string]any{"access_token": "new-access", "token_type": "Bearer", "expires_in": 3599, "scope": scope}), nil
		})
		_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: delegatedCredentials(), Now: refreshTestNow})
		if isAccepted {
			require.NoError(t, err, scope)
			continue
		}
		require.True(t, sdkgo.IsReauthorizationRequired(err), scope)
	}
}

func TestDelegatedRefreshClassifiesTerminalAndRetryableTokenErrors(t *testing.T) {
	for _, test := range []struct {
		status     int
		code       string
		isTerminal bool
	}{
		{status: http.StatusBadRequest, code: "invalid_grant", isTerminal: true},
		{status: http.StatusBadRequest, code: "interaction_required", isTerminal: true},
		{status: http.StatusUnauthorized, code: "invalid_client", isTerminal: true},
		{status: http.StatusServiceUnavailable, code: "temporarily_unavailable"},
		{status: http.StatusInternalServerError, code: "invalid_grant"},
	} {
		driver := newTestDriver(func(*http.Request) (*http.Response, error) {
			return tokenResponse(t, test.status, map[string]any{"error": test.code, "error_description": "AADSTS-SENTINEL"}), nil
		})
		_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: delegatedCredentials(), Now: refreshTestNow})
		require.Error(t, err)
		require.Equal(t, test.isTerminal, sdkgo.IsReauthorizationRequired(err), test.code)
		require.NotContains(t, err.Error(), "AADSTS-SENTINEL")
		require.NotContains(t, err.Error(), testClientSecret)
	}
}

func TestRefreshRequiredReplacesAMissingOrExpiringToken(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	withToken := Credentials{AccessToken: sdkgo.NewSecretString("access")}
	far, near := refreshTestNow.Add(time.Hour), refreshTestNow.Add(4*time.Minute)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withToken, Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withToken, ExpiresAt: &near, Now: refreshTestNow}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withToken, ExpiresAt: &far, Now: refreshTestNow}))
}
