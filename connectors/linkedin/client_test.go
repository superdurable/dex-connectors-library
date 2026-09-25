// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedinconnector_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	linkedinconnector "github.com/superdurable/dex-connectors-library/connectors/linkedin"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var linkedinConnection = sdkgo.ConnectionRef{Provider: "linkedin", Name: "signup"}

func TestGetAuthenticatedProfileReturnsBoundedUserInfoClaims(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v2/userinfo", request.URL.Path)
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		response.Header().Set("X-LI-UUID", "linkedin-request-1")
		writeJSON(t, response, map[string]any{
			"sub": "linkedin-subject", "name": strings.Repeat("n", 300),
			"given_name": "Ada", "family_name": "Lovelace",
			"picture": "https://media.licdn.com/profile.jpg",
			"locale":  map[string]string{"language": "en", "country": "US"},
			"email":   "Ada@Example.COM", "email_verified": true,
			"headline": "must not be copied",
		})
	}))
	defer server.Close()

	client := newClient(t, server.URL+"/v2/userinfo", linkedinconnector.Config{})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "profile-step"), client.GetAuthenticatedProfile(), linkedinConnection,
		linkedinconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, linkedinconnector.GetAuthenticatedProfileBranchProfileLoaded, result.Branch)
	require.Equal(t, "linkedin-subject", result.Value.Subject)
	require.Equal(t, "ada@example.com", result.Value.VerifiedEmail)
	require.Equal(t, "en-US", result.Value.Locale)
	require.Len(t, result.Value.Name, 256)
	require.Equal(t, "linkedin-request-1", result.Receipt.ProviderRequestID)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "one-use-token")
	require.NotContains(t, string(encoded), "must not be copied")
}

func TestGetAuthenticatedProfileRequiresVerifiedEmail(t *testing.T) {
	for _, payload := range []map[string]any{
		{"sub": "member", "email": "member@example.com", "email_verified": false},
		{"sub": "member", "email_verified": true},
		{"sub": "member", "email": "not-an-email", "email_verified": true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			writeJSON(t, response, payload)
		}))
		client := newClient(t, server.URL, linkedinconnector.Config{})
		result, err := sdkgo.RunQuery(
			testsupport.NewDexContext("signup-flow", "email-step"), client.GetAuthenticatedProfile(), linkedinConnection,
			linkedinconnector.GetAuthenticatedProfileInput{},
		)
		server.Close()
		require.NoError(t, err)
		require.Equal(t, linkedinconnector.GetAuthenticatedProfileBranchVerifiedEmailRequired, result.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	}
}

func TestGetAuthenticatedProfileClassifiesTerminalProviderResponses(t *testing.T) {
	tests := []struct {
		status int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{http.StatusUnauthorized, linkedinconnector.GetAuthenticatedProfileBranchAuthorizationRevoked, sdkgo.FailureAuthentication},
		{http.StatusForbidden, linkedinconnector.GetAuthenticatedProfileBranchInsufficientScope, sdkgo.FailureAuthorization},
		{http.StatusNotFound, linkedinconnector.GetAuthenticatedProfileBranchNotFound, sdkgo.FailureNotFound},
		{http.StatusBadRequest, linkedinconnector.GetAuthenticatedProfileBranchProviderRejected, sdkgo.FailureProviderRejection},
		{http.StatusFound, linkedinconnector.GetAuthenticatedProfileBranchProviderRejected, sdkgo.FailureProviderRejection},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("status-%d", test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(`{"error":"sensitive provider response"}`))
			}))
			defer server.Close()
			client := newClient(t, server.URL, linkedinconnector.Config{})
			result, err := sdkgo.RunQuery(
				testsupport.NewDexContext("signup-flow", "terminal-step"), client.GetAuthenticatedProfile(), linkedinConnection,
				linkedinconnector.GetAuthenticatedProfileInput{},
			)
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.NotContains(t, fmt.Sprintf("%#v", result), "sensitive provider response")
		})
	}
}

func TestGetAuthenticatedProfileMissingConnectionUsesDefectBranch(t *testing.T) {
	client, err := linkedinconnector.New(linkedinconnector.Config{}, sdkgo.StaticCredentialProvider[linkedinconnector.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "missing-connection"), client.GetAuthenticatedProfile(), linkedinConnection,
		linkedinconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, linkedinconnector.GetAuthenticatedProfileBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}

func TestRateLimitAndAvailabilityAreSafeRetries(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   sdkgo.FailureKind
		delay  time.Duration
	}{
		{http.StatusTooManyRequests, sdkgo.FailureRateLimit, 7 * time.Second},
		{http.StatusServiceUnavailable, sdkgo.FailureAvailability, 0},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			if test.status == http.StatusTooManyRequests {
				response.Header().Set("Retry-After", "7")
			}
			response.WriteHeader(test.status)
		}))
		client := newClient(t, server.URL, linkedinconnector.Config{})
		_, err := sdkgo.RunQuery(
			testsupport.NewDexContext("signup-flow", "retry-step"), client.GetAuthenticatedProfile(), linkedinConnection,
			linkedinconnector.GetAuthenticatedProfileInput{},
		)
		server.Close()
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, test.kind, retry.Failure.Kind)
		if test.delay > 0 {
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.delay, retryAfter.After)
		}
	}
}

func TestTransportAndResponseBoundsDoNotLeakSecrets(t *testing.T) {
	transportClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer one-use-token", request.Header.Get("Authorization"))
		return nil, errors.New("transport failed with one-use-token")
	})}
	client := newClient(t, "https://api.linkedin.test/v2/userinfo", linkedinconnector.Config{}, linkedinconnector.WithHTTPClient(transportClient))
	_, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "transport-step"), client.GetAuthenticatedProfile(), linkedinConnection,
		linkedinconnector.GetAuthenticatedProfileInput{},
	)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), "one-use-token")

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer server.Close()
	client = newClient(t, server.URL, linkedinconnector.Config{MaxResponseBytes: 16})
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("signup-flow", "large-step"), client.GetAuthenticatedProfile(), linkedinConnection,
		linkedinconnector.GetAuthenticatedProfileInput{},
	)
	require.NoError(t, err)
	require.Equal(t, linkedinconnector.GetAuthenticatedProfileBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestInvalidUserInfoIsTerminalProtocolFailure(t *testing.T) {
	for _, body := range []string{
		`{"email":"member@example.com","email_verified":true}`,
		`{"sub":"member"} trailing`,
		fmt.Sprintf(`{"sub":%q,"email":"member@example.com","email_verified":true}`, strings.Repeat("s", 256)),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte(body))
		}))
		client := newClient(t, server.URL, linkedinconnector.Config{})
		result, err := sdkgo.RunQuery(
			testsupport.NewDexContext("signup-flow", "protocol-step"), client.GetAuthenticatedProfile(), linkedinConnection,
			linkedinconnector.GetAuthenticatedProfileInput{},
		)
		server.Close()
		require.NoError(t, err)
		require.Equal(t, linkedinconnector.GetAuthenticatedProfileBranchInvalidResponse, result.Branch)
		require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	}
}

func TestConfigRejectsUnsafeUserInfoEndpoint(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[linkedinconnector.Credentials]{linkedinConnection: {
		AccessToken: sdkgo.NewSecretString("one-use-token"),
	}}
	_, err := linkedinconnector.New(linkedinconnector.Config{UserInfoURL: "http://linkedin.example/v2/userinfo"}, credentials)
	require.ErrorContains(t, err, "must use HTTPS")
	_, err = linkedinconnector.New(linkedinconnector.Config{UserInfoURL: "https://token@api.linkedin.com/v2/userinfo"}, credentials)
	require.ErrorContains(t, err, "cannot contain user info")
	_, err = linkedinconnector.New(linkedinconnector.Config{UserInfoURL: "https://api.linkedin.com/v2/userinfo?token=value"}, credentials)
	require.ErrorContains(t, err, "cannot contain user info")
}

func newClient(t *testing.T, userInfoURL string, config linkedinconnector.Config, options ...linkedinconnector.Option) *linkedinconnector.Client {
	t.Helper()
	config.UserInfoURL = userInfoURL
	credentials := sdkgo.StaticCredentialProvider[linkedinconnector.Credentials]{linkedinConnection: {
		AccessToken: sdkgo.NewSecretString("one-use-token"),
	}}
	client, err := linkedinconnector.New(config, credentials, options...)
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(response).Encode(value))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
