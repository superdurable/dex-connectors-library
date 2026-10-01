// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func tokenResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestCredentialRefreshUsesTheDataCentersAccountsServerAndKeepsTheRefreshToken(t *testing.T) {
	for _, dataCenter := range desk.DataCenters() {
		t.Run(dataCenter.Name, func(t *testing.T) {
			var form url.Values
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, dataCenter.AccountsURL+"/oauth/v2/token", request.URL.String())
				require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
				require.Empty(t, request.Header.Get("Authorization"), "the client secret travels in the body")
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				form, err = url.ParseQuery(string(body))
				require.NoError(t, err)
				return tokenResponse(http.StatusOK, `{"access_token":"1000.new-access","api_domain":"https://www.zohoapis.com","token_type":"Bearer","expires_in":3600}`), nil
			})}
			now := time.Now()
			result, err := desk.NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[desk.Credentials]{
				Credentials: testCredentials(dataCenter.AuthMethodID), Now: now,
			})
			require.NoError(t, err)
			require.Equal(t, url.Values{
				"grant_type": {"refresh_token"}, "refresh_token": {"1000.zohoDeskTestRefreshToken"},
				"client_id": {"1000.GMB0YULZHJK411248S8I5GZ4CHUEX0"}, "client_secret": {"zoho-client-secret"},
			}, form)
			require.Equal(t, "1000.new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, "1000.zohoDeskTestRefreshToken", result.Credentials.RefreshToken.Reveal(), "Zoho does not rotate refresh tokens")
			require.Equal(t, dataCenter.AuthMethodID, result.Credentials.AuthMethodID)
			require.WithinDuration(t, now.Add(time.Hour), result.ExpiresAt, 5*time.Second)
		})
	}
}

func TestCredentialRefreshNeverTrustsALifetimeLongerThanAnHour(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return tokenResponse(http.StatusOK, `{"access_token":"1000.new-access","token_type":"Bearer","expires_in":3600000}`), nil
	})}
	now := time.Now()
	result, err := desk.NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[desk.Credentials]{
		Credentials: testCredentials(desk.EUDataCenterAuthMethodID), Now: now,
	})
	require.NoError(t, err)
	require.WithinDuration(t, now.Add(time.Hour), result.ExpiresAt, 5*time.Second)
}

func TestCredentialRefreshRequiresReauthorizationOnlyForTerminalErrors(t *testing.T) {
	for name, test := range map[string]struct {
		status         int
		body           string
		isReauthorized bool
	}{
		"revoked refresh token":   {status: http.StatusOK, body: `{"error":"invalid_code"}`, isReauthorized: true},
		"client in another DC":    {status: http.StatusOK, body: `{"error":"invalid_client"}`, isReauthorized: true},
		"wrong client secret":     {status: http.StatusOK, body: `{"error":"invalid_client_secret"}`, isReauthorized: true},
		"throttled":               {status: http.StatusOK, body: `{"error":"Access Denied","error_description":"SENTINEL too many requests"}`},
		"outage with a code":      {status: http.StatusServiceUnavailable, body: `{"error":"invalid_code"}`},
		"missing access token":    {status: http.StatusOK, body: `{"token_type":"Bearer","expires_in":3600}`},
		"HTML maintenance answer": {status: http.StatusOK, body: `<html>SENTINEL</html>`},
	} {
		t.Run(name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return tokenResponse(test.status, test.body), nil })}
			_, err := desk.NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[desk.Credentials]{
				Credentials: testCredentials(desk.INDataCenterAuthMethodID), Now: time.Now(),
			})
			require.Error(t, err)
			require.Equal(t, test.isReauthorized, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), "SENTINEL")
			require.NotContains(t, err.Error(), "zoho-client-secret")
		})
	}
	_, err := desk.NewCredentialRefreshDriver(nil).Refresh(context.Background(), sdkgo.CredentialRefreshState[desk.Credentials]{
		Credentials: testCredentials("zoho-cn-oauth"), Now: time.Now(),
	})
	require.True(t, sdkgo.IsReauthorizationRequired(err), "an unsupported data center cannot be refreshed")
}

func TestCredentialRefreshIsRequiredWithoutATokenOrARecordedExpiry(t *testing.T) {
	driver := desk.NewCredentialRefreshDriver(nil)
	now := time.Now()
	later := now.Add(30 * time.Minute)
	soon := now.Add(30 * time.Second)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[desk.Credentials]{Credentials: testCredentials(desk.USDataCenterAuthMethodID), Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[desk.Credentials]{Credentials: testCredentials(desk.USDataCenterAuthMethodID), Now: now, ExpiresAt: &later}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[desk.Credentials]{Credentials: testCredentials(desk.USDataCenterAuthMethodID), Now: now, ExpiresAt: &soon}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[desk.Credentials]{Credentials: desk.Credentials{AuthMethodID: desk.USDataCenterAuthMethodID}, Now: now, ExpiresAt: &later}))
}

func TestHostedCredentialDecodingAcceptsOnlyTheMethodAndAccessToken(t *testing.T) {
	credentials, err := desk.DecodeResolvedCredentialsJSON(json.RawMessage(`{"auth_method":"zoho-eu-oauth","access_token":"1000.hosted"}`))
	require.NoError(t, err)
	require.Equal(t, desk.EUDataCenterAuthMethodID, credentials.AuthMethodID)
	require.Equal(t, "1000.hosted", credentials.AccessToken.Reveal())
	for name, contents := range map[string]string{
		"refresh token":       `{"auth_method":"zoho-eu-oauth","access_token":"1000.hosted","refresh_token":"SENTINEL"}`,
		"client secret":       `{"auth_method":"zoho-eu-oauth","access_token":"1000.hosted","oauth_client_secret":"SENTINEL"}`,
		"unknown data center": `{"auth_method":"zoho-cn-oauth","access_token":"SENTINEL"}`,
		"missing method":      `{"access_token":"SENTINEL"}`,
		"spaced token":        `{"auth_method":"zoho-eu-oauth","access_token":"SENTINEL token"}`,
	} {
		_, err := desk.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), "SENTINEL", name)
	}
}

func TestCompleteCredentialsRoundTripForTrustedPersistence(t *testing.T) {
	encoded, err := desk.EncodeCredentialsJSON(testCredentials(desk.JPDataCenterAuthMethodID))
	require.NoError(t, err)
	decoded, err := desk.DecodeCredentialsJSON(encoded)
	require.NoError(t, err)
	require.Equal(t, desk.JPDataCenterAuthMethodID, decoded.AuthMethodID)
	require.Equal(t, "1000.zohoDeskTestRefreshToken", decoded.RefreshToken.Reveal())
	_, err = desk.EncodeCredentialsJSON(desk.Credentials{AuthMethodID: desk.JPDataCenterAuthMethodID})
	require.Error(t, err, "incomplete material is never persisted")
}
