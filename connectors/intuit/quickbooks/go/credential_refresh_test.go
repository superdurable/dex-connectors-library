// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// tokenHostTransport sends Intuit's token host to a local fake, as WithLocalProviderURL does for a client.
type tokenHostTransport struct {
	target *url.URL
}

func (transport tokenHostTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewritten := request.Clone(request.Context())
	rewritten.URL.Scheme, rewritten.URL.Host, rewritten.Host = transport.target.Scheme, transport.target.Host, transport.target.Host
	return http.DefaultTransport.RoundTrip(rewritten)
}

func newTestRefreshDriver(t *testing.T, providerURL string) *quickbooks.CredentialRefreshDriver {
	t.Helper()
	target, err := url.Parse(providerURL)
	require.NoError(t, err)
	return quickbooks.NewCredentialRefreshDriver(&http.Client{Transport: tokenHostTransport{target: target}})
}

func refreshState(refreshToken string) sdkgo.CredentialRefreshState[quickbooks.Credentials] {
	return sdkgo.CredentialRefreshState[quickbooks.Credentials]{Credentials: quickbooks.Credentials{
		ClientID: testClientID, ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("old-access"),
		RefreshToken: sdkgo.NewSecretString(refreshToken), IDToken: sdkgo.NewSecretString("id-token-from-consent"),
	}, Now: time.Now()}
}

func TestRefreshExchangesTheRotatingRefreshTokenWithBasicClientAuthentication(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"new-access","expires_in":3600,
			"refresh_token":"rotated-refresh","x_refresh_token_expires_in":8640000}`)
	})
	startedAt := time.Now()
	result, err := newTestRefreshDriver(t, provider.URL).Refresh(context.Background(), refreshState("old-refresh"))
	require.NoError(t, err)
	require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "rotated-refresh", result.Credentials.RefreshToken.Reveal(), "the rotated refresh token replaces the old one")
	require.Equal(t, "id-token-from-consent", result.Credentials.IDToken.Reveal(), "the ID token naming the company is kept")
	require.WithinDuration(t, startedAt.Add(time.Hour), result.ExpiresAt, 5*time.Second)

	request := provider.request(0)
	require.Equal(t, "/oauth2/v1/tokens/bearer", request.path)
	require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte(testClientID+":client-secret")), request.header.Get("Authorization"))
	form, err := url.ParseQuery(request.body)
	require.NoError(t, err)
	require.Equal(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"old-refresh"}}, form)
}

func TestRefreshKeepsThePriorRefreshTokenWhenIntuitOmitsIt(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"new-access","expires_in":3600}`)
	})
	result, err := newTestRefreshDriver(t, provider.URL).Refresh(context.Background(), refreshState("kept-refresh"))
	require.NoError(t, err)
	require.Equal(t, "kept-refresh", result.Credentials.RefreshToken.Reveal())
}

func TestRejectedGrantRequiresReauthorizationAndAnOutageIsRetryable(t *testing.T) {
	for status, body := range map[int]string{http.StatusBadRequest: `{"error":"invalid_grant"}`, http.StatusUnauthorized: `{"error":"invalid_client"}`} {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { writeJSON(t, response, status, body) })
		_, err := newTestRefreshDriver(t, provider.URL).Refresh(context.Background(), refreshState("revoked-refresh"))
		require.True(t, sdkgo.IsReauthorizationRequired(err), body)
		require.NotContains(t, err.Error(), "revoked-refresh")
	}
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"error":"invalid_grant"}`)
	})
	_, err := newTestRefreshDriver(t, provider.URL).Refresh(context.Background(), refreshState("refresh"))
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err), "a 5xx is an outage even with a terminal code")
}

func TestRefreshIsRequiredFiveMinutesBeforeTheHourEnds(t *testing.T) {
	driver := quickbooks.NewCredentialRefreshDriver(nil)
	now := time.Now()
	state := refreshState("refresh")
	state.Now = now
	require.True(t, driver.RefreshRequired(state), "no recorded expiry")
	for expiresIn, isRequired := range map[time.Duration]bool{time.Hour: false, 6 * time.Minute: false, 4 * time.Minute: true, -time.Minute: true} {
		expiresAt := now.Add(expiresIn)
		state.ExpiresAt = &expiresAt
		require.Equal(t, isRequired, driver.RefreshRequired(state), expiresIn)
	}
	state.Credentials.AccessToken = sdkgo.NewSecretString("")
	state.ExpiresAt = nil
	require.True(t, driver.RefreshRequired(state))
}

func TestCompanyRealmComesFromTheIDTokenOrTheRealmIDFallback(t *testing.T) {
	const fallbackRealmID = "4620816365150436710"
	for _, test := range []struct {
		name             string
		idToken          string
		configuredRealm  string
		expectedRealm    string
		isDefectExpected bool
	}{
		{name: "discovery spelling", idToken: testIDToken(t, `"realmid":"`+testRealmID+`"`), expectedRealm: testRealmID},
		{name: "guide spelling", idToken: testIDToken(t, `"realmId":"`+testRealmID+`"`), expectedRealm: testRealmID},
		{name: "matching fallback", idToken: testIDToken(t, `"realmid":"`+testRealmID+`"`), configuredRealm: testRealmID, expectedRealm: testRealmID},
		{name: "token without realm", idToken: testIDToken(t, ""), configuredRealm: fallbackRealmID, expectedRealm: fallbackRealmID},
		{name: "no token", configuredRealm: fallbackRealmID, expectedRealm: fallbackRealmID},
		{name: "unknown company", idToken: testIDToken(t, ""), isDefectExpected: true},
		{name: "another company configured", idToken: testIDToken(t, `"realmid":"`+testRealmID+`"`), configuredRealm: fallbackRealmID, isDefectExpected: true},
		{name: "another app", idToken: encodedIDToken(`{"aud":"another-client","iss":"https://oauth.platform.intuit.com/op/v1","realmid":"` + testRealmID + `"}`),
			isDefectExpected: true},
		{name: "another issuer", idToken: encodedIDToken(`{"aud":"` + testClientID + `","iss":"https://issuer.example.com","realmid":"` + testRealmID + `"}`),
			isDefectExpected: true},
		{name: "single audience", idToken: encodedIDToken(`{"aud":"` + testClientID + `","iss":"https://oauth.platform.intuit.com/op/v1","realmid":"` + testRealmID + `"}`),
			expectedRealm: testRealmID},
		{name: "not a JWT", idToken: "opaque", configuredRealm: fallbackRealmID, isDefectExpected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				invoice := invoiceJSON()
				writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoice})
			})
			client, err := quickbooks.New(quickbooks.Config{RealmID: test.configuredRealm}, staticCredentials(test.idToken), quickbooks.WithLocalProviderURL(provider.URL))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newQuickBooksDexContext("realm-"+test.name), client.GetInvoice(), quickbooksConnection,
				quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
			require.NoError(t, err)
			if test.isDefectExpected {
				require.Equal(t, quickbooks.GetInvoiceBranchDefect, result.Branch)
				require.Zero(t, provider.requestCount(), "no request is sent without the right company")
				return
			}
			require.Equal(t, quickbooks.GetInvoiceBranchFound, result.Branch)
			require.Equal(t, "/v3/company/"+test.expectedRealm+"/invoice/"+testInvoiceID, provider.request(0).path)
		})
	}
}

func TestRejectedUnexpiredAccessTokenIsNotRefreshed(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, faultJSON("AuthenticationFault", "3200"))
	})
	unexpiredAt := time.Now().Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSource(refreshableCredentials(t), &unexpiredAt)
	client, err := quickbooks.New(quickbooks.Config{}, credentials, quickbooks.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("rejected-unexpired"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	stored, _ := credentials.Current()
	require.Equal(t, "refresh", stored.RefreshToken.Reveal())
	require.Equal(t, 1, provider.requestCount(), "a 401 before the recorded expiry is not refreshed or resent")
}

func TestAccessTokenExpiringDuringTheRequestIsRefreshedOnceAndResent(t *testing.T) {
	startedAt := time.Now()
	var isExpiryPassed atomic.Bool
	clock := func() time.Time {
		if isExpiryPassed.Load() {
			return startedAt.Add(2 * time.Hour)
		}
		return startedAt
	}
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		switch {
		case request.path == "/oauth2/v1/tokens/bearer":
			writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"refreshed-access","expires_in":3600,"refresh_token":"rotated"}`)
		case request.header.Get("Authorization") != "Bearer refreshed-access":
			isExpiryPassed.Store(true)
			writeJSON(t, response, http.StatusUnauthorized, faultJSON("AuthenticationFault", "3200"))
		default:
			writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON()})
		}
	})
	expiresAt := startedAt.Add(time.Hour)
	credentials := testsupport.NewRefreshingCredentialSourceWithClock(refreshableCredentials(t), &expiresAt, clock)
	client, err := quickbooks.New(quickbooks.Config{}, credentials, quickbooks.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("refresh"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchFound, result.Branch)
	stored, _ := credentials.Current()
	require.Equal(t, "rotated", stored.RefreshToken.Reveal())
	require.Equal(t, []string{companyPath + "/invoice/" + testInvoiceID, "/oauth2/v1/tokens/bearer", companyPath + "/invoice/" + testInvoiceID},
		[]string{provider.request(0).path, provider.request(1).path, provider.request(2).path}, "one refresh after the 401, then one resend")
	require.Equal(t, 3, provider.requestCount())
}

func TestExpiredAccessTokenIsRefreshedBeforeTheRequest(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.path == "/oauth2/v1/tokens/bearer" {
			writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"refreshed-access","expires_in":3600,"refresh_token":"rotated"}`)
			return
		}
		require.Equal(t, "Bearer refreshed-access", request.header.Get("Authorization"))
		writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON()})
	})
	expiredAt := time.Now().Add(-time.Second)
	credentials := testsupport.NewRefreshingCredentialSource(quickbooks.Credentials{
		ClientID: testClientID, ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken),
		RefreshToken: sdkgo.NewSecretString("refresh"), IDToken: sdkgo.NewSecretString(testIDToken(t, `"realmid":"`+testRealmID+`"`)),
	}, &expiredAt)
	client, err := quickbooks.New(quickbooks.Config{}, credentials, quickbooks.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("expired"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchFound, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}

// refreshableCredentials are consent credentials whose ID token names testRealmID.
func refreshableCredentials(t *testing.T) quickbooks.Credentials {
	t.Helper()
	return quickbooks.Credentials{
		ClientID: testClientID, ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken),
		RefreshToken: sdkgo.NewSecretString("refresh"), IDToken: sdkgo.NewSecretString(testIDToken(t, `"realmid":"`+testRealmID+`"`)),
	}
}
