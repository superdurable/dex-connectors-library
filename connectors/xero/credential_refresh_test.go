// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// testClientID is split so the 32-hex fixture is never one credential-shaped literal.
	testClientID       = "0F1E2D3C4B5A6978" + "8796A5B4C3D2E1F0"
	testClientSecret   = "client-secret"
	grantedXeroScopes  = "accounting.invoices accounting.payments accounting.contacts.read"
	testDecodedInvoice = "243216c5-369e-4056-ac67-05388f86dc81"
)

type tokenRoundTripFunc func(*http.Request) (*http.Response, error)

func (function tokenRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCustomConnectionRequestsTheClientCredentialsGrantWithBasicAuthentication(t *testing.T) {
	now := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: tokenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, xeroTokenEndpoint, request.URL.String())
		clientID, clientSecret, hasBasicAuthentication := request.BasicAuth()
		require.True(t, hasBasicAuthentication, "Xero documents HTTP Basic client authentication")
		require.Equal(t, testClientID, clientID)
		require.Equal(t, testClientSecret, clientSecret)
		require.NoError(t, request.ParseForm())
		require.Equal(t, "client_credentials", request.PostForm.Get("grant_type"))
		require.Equal(t, grantedXeroScopes, request.PostForm.Get("scope"), "exactly the connector's accounting scopes")
		require.Empty(t, request.PostForm.Get("client_secret"), "the secret travels only in the Basic header")
		return tokenResponseForTest(t, http.StatusOK, map[string]any{
			"access_token": "custom-access", "expires_in": 1800, "token_type": "Bearer", "scope": grantedXeroScopes,
		}), nil
	})}
	driver := NewCredentialRefreshDriver(client)
	driver.now = func() time.Time { return now }
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: customConnectionCredentials(), Now: now})
	require.NoError(t, err)
	require.Equal(t, "custom-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, testClientSecret, result.Credentials.ClientSecret.Reveal())
	require.Empty(t, result.Credentials.RefreshToken.Reveal())
	require.Equal(t, now.Add(30*time.Minute), result.ExpiresAt)
}

func TestOAuthRefreshUsesBasicAuthenticationAndKeepsTheRotatedRefreshToken(t *testing.T) {
	now := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name            string
		rotatedRefresh  string
		expectedRefresh string
		scope           string
	}{
		{name: "persists the rotated refresh token", rotatedRefresh: "rotated-refresh", expectedRefresh: "rotated-refresh", scope: "offline_access " + grantedXeroScopes},
		{name: "keeps the prior token when Xero omits one", expectedRefresh: "existing-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: tokenRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				_, _, hasBasicAuthentication := request.BasicAuth()
				require.True(t, hasBasicAuthentication)
				require.NoError(t, request.ParseForm())
				require.Equal(t, "refresh_token", request.PostForm.Get("grant_type"))
				require.Equal(t, "existing-refresh", request.PostForm.Get("refresh_token"))
				require.Empty(t, request.PostForm.Get("scope"))
				body := map[string]any{"access_token": "new-access", "token_type": "Bearer", "expires_in": 1800}
				if test.rotatedRefresh != "" {
					body["refresh_token"] = test.rotatedRefresh
				}
				if test.scope != "" {
					body["scope"] = test.scope
				}
				return tokenResponseForTest(t, http.StatusOK, body), nil
			})}
			driver := NewCredentialRefreshDriver(client)
			driver.now = func() time.Time { return now }
			result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: oauthCredentials(), Now: now})
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, test.expectedRefresh, result.Credentials.RefreshToken.Reveal())
			require.Equal(t, now.Add(30*time.Minute), result.ExpiresAt)
		})
	}
}

func TestTokenRejectionsRequireReauthorizationAndOutagesRetry(t *testing.T) {
	for _, test := range []struct {
		name                       string
		credentials                Credentials
		status                     int
		body                       map[string]any
		isReauthorizationRequired  bool
		isProviderContactAvoidable bool
	}{
		{name: "replaced secret", credentials: customConnectionCredentials(), status: http.StatusBadRequest,
			body: map[string]any{"error": "invalid_client"}, isReauthorizationRequired: true},
		{name: "scope removed from the custom connection", credentials: customConnectionCredentials(), status: http.StatusBadRequest,
			body: map[string]any{"error": "invalid_scope"}, isReauthorizationRequired: true},
		{name: "expired refresh token", credentials: oauthCredentials(), status: http.StatusBadRequest,
			body: map[string]any{"error": "invalid_grant"}, isReauthorizationRequired: true},
		{name: "granted scope without payments", credentials: customConnectionCredentials(), status: http.StatusOK,
			body:                      map[string]any{"access_token": "a", "token_type": "Bearer", "expires_in": 1800, "scope": "accounting.invoices accounting.contacts.read"},
			isReauthorizationRequired: true},
		{name: "identity outage", credentials: customConnectionCredentials(), status: http.StatusServiceUnavailable,
			body: map[string]any{"error": "invalid_client"}},
		{name: "missing secret", credentials: Credentials{AuthMethodID: CustomConnectionAuthMethodID, ClientID: testClientID},
			isReauthorizationRequired: true, isProviderContactAvoidable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: tokenRoundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				return tokenResponseForTest(t, test.status, test.body), nil
			})}
			_, err := NewCredentialRefreshDriver(client).Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{
				Credentials: test.credentials, Now: time.Now(),
			})
			require.Error(t, err)
			require.Equal(t, test.isReauthorizationRequired, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), testClientSecret)
			if test.isProviderContactAvoidable {
				require.Zero(t, requests)
			}
		})
	}
}

func TestRefreshIsRequiredForMissingOrExpiringTokens(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	now := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	credentials := customConnectionCredentials()
	credentials.AccessToken = sdkgo.NewSecretString("access")
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{AuthMethodID: CustomConnectionAuthMethodID}, Now: now}),
		"a Custom Connection saved without a token mints one first")
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, Now: now}), "Xero issues only expiring tokens")
	insideSkew, beyondSkew := now.Add(5*time.Minute), now.Add(5*time.Minute+time.Second)
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &insideSkew, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials, ExpiresAt: &beyondSkew, Now: now}))
}

// TestCustomConnectionMintsStoresAndReplacesARejectedToken starts from the Custom Connection credentials
// Dex Web saves, which have no access token, and a provider that later rejects the token.
func TestCustomConnectionMintsStoresAndReplacesARejectedToken(t *testing.T) {
	var mutex sync.Mutex
	issued, apiCalls := 0, 0
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/connect/token" {
			issued++
			writeTestJSON(t, response, http.StatusOK, map[string]any{
				"access_token": "minted-" + strconv.Itoa(issued), "expires_in": 1800, "token_type": "Bearer", "scope": grantedXeroScopes,
			})
			return
		}
		apiCalls++
		if request.Header.Get("Xero-Tenant-Id") != "" {
			t.Errorf("a Custom Connection must not send a tenant")
		}
		if request.Header.Get("Authorization") != "Bearer minted-2" {
			writeTestJSON(t, response, http.StatusUnauthorized, map[string]any{"Detail": "TokenExpired"})
			return
		}
		writeTestJSON(t, response, http.StatusOK, map[string]any{"Invoices": []any{map[string]any{
			"InvoiceID": testDecodedInvoice, "Type": "ACCREC", "Status": "DRAFT", "Total": 1.5,
		}}})
	}))
	t.Cleanup(provider.Close)
	credentials := testsupport.NewRefreshingCredentialSource(customConnectionCredentials(), nil)
	client, err := New(Config{}, credentials, WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	connection, err := NewConnection(client, sdkgo.ConnectionRef{Provider: "xero", Name: "xero-books"})
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newInternalDexContext(), connection.client.GetInvoice(), connection.reference, GetInvoiceInput{InvoiceID: testDecodedInvoice})
	require.NoError(t, err)
	require.Equal(t, GetInvoiceBranchFound, result.Branch, "the first token was rejected, replaced once, and the call resent")
	require.Equal(t, Decimal("1.5"), result.Value.Total)
	require.Equal(t, 2, issued)
	require.Equal(t, 2, apiCalls)

	stored, expiresAt := credentials.Current()
	require.Equal(t, "minted-2", stored.AccessToken.Reveal())
	require.Equal(t, CustomConnectionAuthMethodID, stored.AuthMethodID)
	require.Equal(t, testClientSecret, stored.ClientSecret.Reveal())
	require.NotNil(t, expiresAt)
	require.True(t, expiresAt.After(time.Now().Add(25*time.Minute)))
}

func customConnectionCredentials() Credentials {
	return Credentials{
		AuthMethodID: CustomConnectionAuthMethodID, ClientID: testClientID, ClientSecret: sdkgo.NewSecretString(testClientSecret),
	}
}

func oauthCredentials() Credentials {
	return Credentials{
		AuthMethodID: OAuthAuthMethodID, ClientID: testClientID, ClientSecret: sdkgo.NewSecretString(testClientSecret),
		AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("existing-refresh"),
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

func writeTestJSON(t *testing.T, response http.ResponseWriter, status int, body map[string]any) {
	t.Helper()
	response.WriteHeader(status)
	require.NoError(t, json.NewEncoder(response).Encode(body))
}
