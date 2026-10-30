// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func tokenResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func tokenClient(t *testing.T, expectedURL string, body string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, expectedURL, request.URL.String())
		return tokenResponse(http.StatusOK, body), nil
	})}
}

func TestCredentialRefreshUsesTheDataCentersAccountsServerAndStoresItsAPIDomain(t *testing.T) {
	for _, dataCenter := range crm.DataCenters() {
		t.Run(dataCenter.Name, func(t *testing.T) {
			var form url.Values
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, dataCenter.AccountsURL+"/oauth/v2/token", request.URL.String())
				require.Empty(t, request.Header.Get("Authorization"), "the client secret travels in the body")
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				form, err = url.ParseQuery(string(body))
				require.NoError(t, err)
				return tokenResponse(http.StatusOK, `{"access_token":"1000.new-access","api_domain":"https://sandbox.`+dataCenter.APIDomainSuffix+`","token_type":"Bearer","expires_in":3600}`), nil
			})}
			credentials := testCredentials(dataCenter.AuthMethodID)
			credentials.APIDomain = ""
			now := time.Now()
			result, err := crm.NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, Now: now})
			require.NoError(t, err)
			require.Equal(t, url.Values{
				"grant_type": {"refresh_token"}, "refresh_token": {testRefreshToken},
				"client_id": {"1000.GMB0YULZHJK411248S8I5GZ4CHUEX0"}, "client_secret": {"zoho-client-secret"},
			}, form)
			require.Equal(t, "1000.new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, testRefreshToken, result.Credentials.RefreshToken.Reveal(), "Zoho does not rotate refresh tokens")
			require.Equal(t, "https://sandbox."+dataCenter.APIDomainSuffix, result.Credentials.APIDomain)
			require.WithinDuration(t, now.Add(time.Hour), result.ExpiresAt, 5*time.Second)
		})
	}
}

func TestCredentialRefreshKeepsOrDefaultsTheAPIDomainAndNeverStoresAForeignOne(t *testing.T) {
	driver := crm.NewCredentialRefreshDriver(tokenClient(t, "https://accounts.zoho.eu/oauth/v2/token",
		`{"access_token":"1000.new-access","token_type":"Bearer","expires_in":3600000}`))
	credentials := testCredentials(crm.EUDataCenterAuthMethodID)
	credentials.APIDomain = "https://developer.zohoapis.eu"
	now := time.Now()
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, Now: now})
	require.NoError(t, err)
	require.Equal(t, "https://developer.zohoapis.eu", result.Credentials.APIDomain, "a response without api_domain keeps the stored one")
	require.WithinDuration(t, now.Add(time.Hour), result.ExpiresAt, 5*time.Second, "a lifetime longer than an hour is never trusted")

	credentials.APIDomain = ""
	result, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, Now: now})
	require.NoError(t, err)
	require.Equal(t, "https://www.zohoapis.eu", result.Credentials.APIDomain)

	foreign := crm.NewCredentialRefreshDriver(tokenClient(t, "https://accounts.zoho.eu/oauth/v2/token",
		`{"access_token":"1000.new-access","api_domain":"https://www.zohoapis.com","token_type":"Bearer","expires_in":3600}`))
	_, err = foreign.Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, Now: now})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "a foreign api_domain is retried, never stored")
}

func TestCredentialRefreshRequiresReauthorizationForTerminalZohoCodes(t *testing.T) {
	for _, code := range []string{"invalid_code", "invalid_client", "invalid_client_secret"} {
		driver := crm.NewCredentialRefreshDriver(tokenClient(t, "https://accounts.zoho.com/oauth/v2/token", `{"error":"`+code+`"}`))
		_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: testCredentials(crm.USDataCenterAuthMethodID), Now: time.Now()})
		require.True(t, sdkgo.IsReauthorizationRequired(err), code)
		require.NotContains(t, err.Error(), testRefreshToken)
	}
	throttled := crm.NewCredentialRefreshDriver(tokenClient(t, "https://accounts.zoho.com/oauth/v2/token", `{"error":"Access Denied"}`))
	_, err := throttled.Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: testCredentials(crm.USDataCenterAuthMethodID), Now: time.Now()})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "Zoho's token throttle is retried")
	_, err = crm.NewCredentialRefreshDriver(nil).Refresh(context.Background(), sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: testCredentials("zoho-cn-oauth")})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

func TestRefreshIsRequiredWithoutAnAPIDomainOrNearExpiry(t *testing.T) {
	driver := crm.NewCredentialRefreshDriver(nil)
	now := time.Now()
	later := now.Add(30 * time.Minute)
	credentials := testCredentials(crm.USDataCenterAuthMethodID)
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, Now: now}), "no recorded expiry")
	soon := now.Add(time.Minute)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, ExpiresAt: &soon, Now: now}))
	credentials.APIDomain = ""
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[crm.Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}),
		"a connection without api_domain refreshes once to obtain it")
}
