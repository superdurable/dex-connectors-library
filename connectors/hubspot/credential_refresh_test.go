// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const allGrantedScopes = `["oauth","crm.objects.contacts.read","crm.objects.contacts.write","crm.objects.companies.read",
	"crm.objects.companies.write","crm.objects.deals.read","crm.objects.deals.write","crm.objects.owners.read"]`

func TestCredentialRefreshDriverRefreshesOAuthAndAlwaysCarriesTheNextRefreshToken(t *testing.T) {
	for _, test := range []struct {
		name            string
		rotatedRefresh  string
		expectedRefresh string
	}{
		{name: "keeps the prior refresh token when HubSpot omits one", expectedRefresh: "stored-refresh-token"},
		{name: "persists a rotated refresh token", rotatedRefresh: "rotated-refresh-token", expectedRefresh: "rotated-refresh-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
			driver := hubspot.NewCredentialRefreshDriver(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
				_, _, hasBasicAuthentication := request.BasicAuth()
				require.False(t, hasBasicAuthentication, "HubSpot takes client credentials only in the form body")
				require.NoError(t, request.ParseForm())
				require.Empty(t, request.URL.RawQuery, "no secret travels in the query string")
				require.Equal(t, "refresh_token", request.PostForm.Get("grant_type"))
				require.Equal(t, "stored-refresh-token", request.PostForm.Get("refresh_token"))
				require.Equal(t, "client-id", request.PostForm.Get("client_id"))
				require.Equal(t, "client-secret", request.PostForm.Get("client_secret"))
				body := map[string]any{
					"token_type": "bearer", "access_token": "new-access-token", "expires_in": 1800, "hub_id": 1234567,
					"scopes": json.RawMessage(allGrantedScopes),
				}
				if test.rotatedRefresh != "" {
					body["refresh_token"] = test.rotatedRefresh
				}
				contents, err := json.Marshal(body)
				require.NoError(t, err)
				writeJSON(t, response, http.StatusOK, string(contents))
			}}})
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[hubspot.Credentials]{
				Credentials: oauthCredentials(), Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, "new-access-token", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, hubspot.OAuthAuthMethodID, result.Credentials.AuthMethodID)
			require.WithinDuration(t, time.Now().Add(30*time.Minute), result.ExpiresAt, time.Minute)
		})
	}
}

func TestCredentialRefreshDriverRequiresReauthorizationForBadRefreshTokenWithoutProviderText(t *testing.T) {
	driver := hubspot.NewCredentialRefreshDriver(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant","status":"BAD_REFRESH_TOKEN","message":"`+providerMessageSentinel+`","correlationId":"x"}`)
	}}})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: oauthCredentials()})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), providerMessageSentinel)
}

func TestCredentialRefreshDriverRequiresReauthorizationWhenACRMScopeIsMissing(t *testing.T) {
	driver := hubspot.NewCredentialRefreshDriver(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"new-access-token","expires_in":1800,
			"scopes":["oauth","crm.objects.contacts.read","crm.objects.contacts.write"]}`)
	}}})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: oauthCredentials()})
	require.Error(t, err)
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "new-access-token")
}

func TestCredentialRefreshDriverTreatsServerFailureAsRetryable(t *testing.T) {
	driver := hubspot.NewCredentialRefreshDriver(&http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"error":"invalid_grant"}`)
	}}})
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: oauthCredentials()})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "HubSpot can report invalid_grant during an outage")
}

func TestCredentialRefreshDriverNeverRefreshesAPrivateAppToken(t *testing.T) {
	driver := hubspot.NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	expired := now.Add(-time.Hour)
	privateAppToken := hubspot.Credentials{AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)}
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: privateAppToken, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: privateAppToken, ExpiresAt: &expired, Now: now}))
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: privateAppToken, Now: now})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "a private app token is never marked for reauthorization by refresh")
}

func TestCredentialRefreshDriverRefreshesOAuthWithinTheExpirySkew(t *testing.T) {
	driver := hubspot.NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.September, 30, 8, 0, 0, 0, time.UTC)
	credentials := oauthCredentials()
	insideSkew := now.Add(5 * time.Minute)
	beyondSkew := insideSkew.Add(time.Nanosecond)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: credentials, Now: now}), "an OAuth token without expiry is refreshed")
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: credentials, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[hubspot.Credentials]{Credentials: credentials, ExpiresAt: &beyondSkew, Now: now}))
}

func TestDecodeResolvedCredentialsAcceptsOnlyTheShortLivedToken(t *testing.T) {
	credentials, err := hubspot.DecodeResolvedCredentialsJSON(json.RawMessage(`{"auth_method":"hubspot-oauth","access_token":"short-lived"}`))
	require.NoError(t, err)
	require.Equal(t, "short-lived", credentials.AccessToken.Reveal())
	require.Equal(t, hubspot.OAuthAuthMethodID, credentials.AuthMethodID)
	for _, contents := range []string{
		`{"access_token":"short-lived","refresh_token":"must-not-cross-the-broker"}`,
		`{"auth_method":"api-key","access_token":"short-lived"}`,
		`{"auth_method":"private-app-token"}`,
	} {
		_, err := hubspot.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "must-not-cross-the-broker")
	}
}

func TestLocalCredentialsRoundTripForEachAuthMethod(t *testing.T) {
	for _, credentials := range []hubspot.Credentials{
		{AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)},
		oauthCredentials(),
	} {
		encoded, err := hubspot.EncodeCredentialsJSON(credentials)
		require.NoError(t, err)
		decoded, err := hubspot.DecodeCredentialsJSON(encoded)
		require.NoError(t, err)
		require.Equal(t, credentials.AuthMethodID, decoded.AuthMethodID)
		require.Equal(t, credentials.AccessToken.Reveal(), decoded.AccessToken.Reveal())
		require.Equal(t, credentials.RefreshToken.Reveal(), decoded.RefreshToken.Reveal())
	}
	_, err := hubspot.EncodeCredentialsJSON(hubspot.Credentials{AuthMethodID: hubspot.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("a")})
	require.Error(t, err, "an OAuth credential without client and refresh material is incomplete")
}

func oauthCredentials() hubspot.Credentials {
	return hubspot.Credentials{
		AuthMethodID: hubspot.OAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("old-access-token"), RefreshToken: sdkgo.NewSecretString("stored-refresh-token"),
	}
}
