// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/connectors/linear/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// tokenEndpointClient sends api.linear.app token requests to tokenServer and every other request unchanged.
func tokenEndpointClient(t *testing.T, tokenServer *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(tokenServer.URL)
	require.NoError(t, err)
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.linear.app" {
			redirected := request.Clone(request.Context())
			redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
			return http.DefaultTransport.RoundTrip(redirected)
		}
		return http.DefaultTransport.RoundTrip(request)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func oauthCredentials(accessToken string) linear.Credentials {
	return linear.Credentials{
		AuthMethodID: linear.OAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(accessToken), RefreshToken: sdkgo.NewSecretString("refresh-1"),
	}
}

func TestRejectedOAuthTokenIsRefreshedAndTheRequestResentOnce(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/oauth/token", request.URL.Path)
		writeJSON(t, response, http.StatusOK, `{"access_token":"`+testAccessToken+`-2","refresh_token":"refresh-2","token_type":"Bearer","expires_in":86399,"scope":"read write"}`)
	}))
	t.Cleanup(tokenServer.Close)
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.header.Get("Authorization") != "Bearer "+testAccessToken+"-2" {
			writeJSON(t, response, http.StatusUnauthorized, linearError("AUTHENTICATION_ERROR", "authentication error"))
			return
		}
		writeData(t, response, map[string]any{"users": map[string]any{"nodes": []any{}}})
	})
	// The stored expiry is an hour away, so only Linear's 401 makes the connector refresh.
	source := testsupport.NewRefreshingCredentialSource(oauthCredentials(testAccessToken+"-1"), timePointer(time.Now().Add(time.Hour)))
	client := newLinearClient(t, provider.URL, source, linear.WithHTTPClient(tokenEndpointClient(t, tokenServer)))
	result, err := runQuery("rejected", client.FindUserByEmail(), linear.FindUserByEmailInput{Email: "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, linear.FindUserByEmailBranchNotFound, result.Branch, "the resent request ran")
	require.Equal(t, 2, provider.requestCount())
	current, expiresAt := source.Current()
	require.Equal(t, "refresh-2", current.RefreshToken.Reveal())
	require.True(t, expiresAt.After(time.Now().Add(23*time.Hour)))
}

func TestASecondRejectionSelectsProviderRejectedWithoutALoop(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusOK, `{"access_token":"still-rejected","refresh_token":"refresh-2","token_type":"Bearer","expires_in":86399}`)
	}))
	t.Cleanup(tokenServer.Close)
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, linearError("AUTHENTICATION_ERROR", "authentication error"))
	})
	source := testsupport.NewRefreshingCredentialSource(oauthCredentials(testAccessToken), timePointer(time.Now().Add(time.Hour)))
	client := newLinearClient(t, provider.URL, source, linear.WithHTTPClient(tokenEndpointClient(t, tokenServer)))
	result, err := runQuery("twice", client.FindUserByEmail(), linear.FindUserByEmailInput{Email: "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, linear.FindUserByEmailBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 2, provider.requestCount(), "one refresh, one resend, no loop")
}

func TestARevokedGrantRequiresReauthorization(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"SENTINEL revoked"}`)
	}))
	t.Cleanup(tokenServer.Close)
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	source := testsupport.NewRefreshingCredentialSource(oauthCredentials(testAccessToken), timePointer(time.Now().Add(-time.Minute)))
	client := newLinearClient(t, provider.URL, source, linear.WithHTTPClient(tokenEndpointClient(t, tokenServer)))
	result, err := runQuery("revoked", client.FindUserByEmail(), linear.FindUserByEmailInput{Email: "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, linear.FindUserByEmailBranchProviderRejected, result.Branch)
	require.Equal(t, "Linear authorization must be renewed in Dex Web Connections", result.Failure.Message)
	require.True(t, source.IsReauthorizationRequired())
}

func timePointer(value time.Time) *time.Time { return &value }
