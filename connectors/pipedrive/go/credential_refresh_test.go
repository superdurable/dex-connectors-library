// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCredentialRefreshDriverUsesBasicClientAuthenticationAndKeepsTheAPIDomain(t *testing.T) {
	now := time.Date(2026, time.October, 4, 8, 0, 0, 0, time.UTC)
	driver := pipedrive.NewCredentialRefreshDriver(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		clientID, clientSecret, hasBasicAuthentication := request.BasicAuth()
		require.True(t, hasBasicAuthentication, "Pipedrive recommends HTTP Basic client authentication")
		require.Equal(t, "client-id", clientID)
		require.Equal(t, "client-secret", clientSecret)
		require.NoError(t, request.ParseForm())
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "refresh_token", request.PostForm.Get("grant_type"))
		require.Equal(t, "stored-refresh-token", request.PostForm.Get("refresh_token"))
		require.Empty(t, request.PostForm.Get("client_secret"), "the secret travels only in the Authorization header")
		writeJSON(t, response, http.StatusOK, `{"access_token":"v1u:new-access","token_type":"Bearer","expires_in":3599,
			"refresh_token":"stored-refresh-token","scope":"base,deals:full,contacts:full,users:read","api_domain":"https://acme.pipedrive.com"}`)
	}}})
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[pipedrive.Credentials]{
		Credentials: oauthCredentials("https://old.pipedrive.com"), Now: now,
	})
	require.NoError(t, err)
	require.Equal(t, "v1u:new-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "stored-refresh-token", result.Credentials.RefreshToken.Reveal())
	require.Equal(t, "https://acme.pipedrive.com", result.Credentials.APIDomain, "the company API domain follows every refresh")
	require.Equal(t, pipedrive.OAuthAuthMethodID, result.Credentials.AuthMethodID)
	require.False(t, result.ExpiresAt.Before(time.Now().Add(59*time.Minute)), "the expiry is the hour Pipedrive grants")
}

func TestCredentialRefreshDriverClassifiesTerminalAndRetryableFailures(t *testing.T) {
	for _, test := range []struct {
		name                         string
		status                       int
		body                         string
		isReauthorizationRequired    bool
		isRefreshTemporarilyRejected bool
	}{
		{name: "revoked refresh token", status: http.StatusBadRequest, body: `{"success":false,"message":"` + providerMessageSentinel + `","error":"invalid_grant"}`, isReauthorizationRequired: true},
		{name: "deleted app", status: http.StatusUnauthorized, body: `{"success":false,"message":"` + providerMessageSentinel + `","error":"invalid_client"}`, isReauthorizationRequired: true},
		{name: "scope removed from the app", status: http.StatusOK, body: `{"access_token":"a","token_type":"Bearer","expires_in":3599,"scope":"base,deals:full","api_domain":"https://acme.pipedrive.com"}`, isReauthorizationRequired: true},
		{name: "outage", status: http.StatusServiceUnavailable, body: `{"error":"invalid_grant"}`, isRefreshTemporarilyRejected: true},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"rate_limited"}`, isRefreshTemporarilyRejected: true},
		{name: "foreign API domain", status: http.StatusOK, body: `{"access_token":"a","token_type":"Bearer","expires_in":3599,"api_domain":"https://acme.example.com"}`, isRefreshTemporarilyRejected: true},
		{name: "missing API domain", status: http.StatusOK, body: `{"access_token":"a","token_type":"Bearer","expires_in":3599}`, isRefreshTemporarilyRejected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := pipedrive.NewCredentialRefreshDriver(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, test.status, test.body)
			}}})
			_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[pipedrive.Credentials]{
				Credentials: oauthCredentials("https://acme.pipedrive.com"), Now: time.Now(),
			})
			require.Error(t, err)
			require.NotContains(t, err.Error(), providerMessageSentinel)
			require.NotContains(t, err.Error(), "client-secret")
			require.Equal(t, test.isReauthorizationRequired, sdkgo.IsReauthorizationRequired(err))
			require.Equal(t, test.isRefreshTemporarilyRejected, !sdkgo.IsReauthorizationRequired(err))
		})
	}
	_, err := pipedrive.NewCredentialRefreshDriver(nil).Refresh(context.Background(), sdkgo.CredentialRefreshState[pipedrive.Credentials]{
		Credentials: pipedrive.Credentials{AuthMethodID: pipedrive.APITokenAuthMethodID, APIToken: sdkgo.NewSecretString(testAPIToken)},
	})
	require.Error(t, err, "a Personal API token is never refreshed")
	require.False(t, errors.Is(err, sdkgo.ErrReauthorizationRequired))
}

func TestCredentialRefreshDriverRefreshesOnlyExpiringOAuthTokensOrAMissingAPIDomain(t *testing.T) {
	driver := pipedrive.NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.October, 4, 8, 0, 0, 0, time.UTC)
	later, soon := now.Add(time.Hour), now.Add(2*time.Minute)
	for name, test := range map[string]struct {
		credentials pipedrive.Credentials
		expiresAt   *time.Time
		isRequired  bool
	}{
		"API token":                {credentials: pipedrive.Credentials{AuthMethodID: pipedrive.APITokenAuthMethodID}, isRequired: false},
		"fresh OAuth token":        {credentials: oauthCredentials("https://acme.pipedrive.com"), expiresAt: &later, isRequired: false},
		"OAuth token expiring":     {credentials: oauthCredentials("https://acme.pipedrive.com"), expiresAt: &soon, isRequired: true},
		"OAuth token no expiry":    {credentials: oauthCredentials("https://acme.pipedrive.com"), isRequired: true},
		"OAuth without api_domain": {credentials: oauthCredentials(""), expiresAt: &later, isRequired: true},
	} {
		require.Equal(t, test.isRequired, driver.RefreshRequired(sdkgo.CredentialRefreshState[pipedrive.Credentials]{
			Credentials: test.credentials, ExpiresAt: test.expiresAt, Now: now,
		}), name)
	}
}
