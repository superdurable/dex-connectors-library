// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewValidatesRegionLimitsAndOptions(t *testing.T) {
	credentials := testCredentialProvider()
	_, err := intercom.New(intercom.Config{Region: "ca"}, credentials)
	require.Error(t, err)
	_, err = intercom.New(intercom.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = intercom.New(intercom.Config{WebhookMaxBodyBytes: -1}, credentials)
	require.Error(t, err)
	_, err = intercom.New(intercom.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = intercom.New(intercom.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	_, err = intercom.New(intercom.Config{}, credentials, intercom.WithClock(nil))
	require.ErrorContains(t, err, "clock is required")
	for _, baseURL := range []string{"http://intercom.example.com", "https://user:secret@api.intercom.io", "https://api.intercom.io?x=1"} {
		_, err = intercom.New(intercom.Config{}, credentials, intercom.WithAPIBaseURL(baseURL))
		require.Error(t, err, baseURL)
		require.NotContains(t, err.Error(), "secret")
	}
	for _, region := range []intercom.Region{"", intercom.RegionUs, intercom.RegionEu, intercom.RegionAu} {
		client, err := intercom.New(intercom.Config{Region: region}, credentials)
		require.NoError(t, err, region)
		require.NotNil(t, client)
	}
}

// TestRegionSelectsTheRegionalAPIHost records the request URL each region sends, without a network call.
func TestRegionSelectsTheRegionalAPIHost(t *testing.T) {
	for region, host := range map[intercom.Region]string{
		"": "api.intercom.io", intercom.RegionUs: "api.intercom.io", intercom.RegionEu: "api.eu.intercom.io", intercom.RegionAu: "api.au.intercom.io",
	} {
		var requestedURL string
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requestedURL = request.URL.String()
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"type":"error.list","errors":[{"code":"not_found"}]}`)), Request: request}, nil
		})
		client, err := intercom.New(intercom.Config{Region: region}, testCredentialProvider(), intercom.WithHTTPClient(&http.Client{Transport: transport}))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newTestDexContext("region-"+string(region)), client.GetConversation(), intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
		require.NoError(t, err)
		require.Equal(t, intercom.GetConversationBranchNotFound, result.Branch)
		require.Equal(t, "https://"+host+"/conversations/"+testConversation+"?display_as=plaintext", requestedURL, region)
	}
}

func TestRequestsSendTheBearerTokenAndAPIVersion(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, conversationJSON(t, testConversation, "open", nil))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"type":"list","data":[],"total_count":0,"pages":{"type":"pages"}}`)
	})
	client := newIntercomClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newTestDexContext("headers"), client.GetConversation(), intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newTestDexContext("post-headers"), client.FindContactByEmail(), intercomConnection, intercom.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	for index := range 2 {
		request := provider.request(index)
		require.Equal(t, "Bearer "+testAccessToken, request.header.Get("Authorization"))
		require.Equal(t, "application/json", request.header.Get("Accept"))
		require.Equal(t, intercom.APIVersion, request.header.Get("Intercom-Version"))
		require.Equal(t, "2.16", intercom.APIVersion)
	}
	require.Empty(t, provider.request(0).header.Get("Content-Type"))
	require.Equal(t, "application/json", provider.request(1).header.Get("Content-Type"))
}

func TestFailureStatusesSelectBranchesWithOnlyIntercomErrorCodes(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		message string
	}{
		{
			name: "unauthorized", status: http.StatusUnauthorized,
			body:   `{"type":"error.list","request_id":"f93ecfa8-d08a-4325-8694-89aeb89c8f85","errors":[{"code":"unauthorized","message":"SENTINEL Access Token Invalid"}]}`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureAuthentication,
			message: "Intercom rejected the access token (HTTP 401); check the token and that the region matches the workspace [unauthorized]",
		},
		{
			name: "forbidden", status: http.StatusForbidden, body: `{"type":"error.list","errors":[{"code":"action_forbidden","message":"SENTINEL"}]}`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureAuthorization, message: "Intercom rejected the request (HTTP 403) [action_forbidden]",
		},
		{
			name: "plan restricted", status: http.StatusPaymentRequired, body: `{"type":"error.list","errors":[{"code":"api_plan_restricted"}]}`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "Intercom rejected the request (HTTP 402) [api_plan_restricted]",
		},
		{
			name: "not found", status: http.StatusNotFound, body: `{"type":"error.list","errors":[{"code":"not_found","message":"SENTINEL Resource Not Found"}]}`,
			branch: intercom.GetConversationBranchNotFound, kind: sdkgo.FailureNotFound, message: "Intercom found no such resource (HTTP 404) [not_found]",
		},
		{
			name: "gone", status: http.StatusGone, body: `{"type":"error.list","errors":[{"code":"contact_merged"}]}`,
			branch: intercom.GetConversationBranchNotFound, kind: sdkgo.FailureNotFound, message: "Intercom found no such resource (HTTP 410) [contact_merged]",
		},
		{
			name: "invalid parameter", status: http.StatusUnprocessableEntity,
			body:   `{"type":"error.list","errors":[{"code":"parameter_invalid","field":"admin_id","message":"SENTINEL Admin not found"},{"code":"Bad Code"}]}`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Intercom rejected the request (HTTP 422) [parameter_invalid; fields: admin_id]",
		},
		{
			name: "conflict", status: http.StatusConflict, body: `{"type":"error.list","errors":[{"code":"conflict"}]}`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureConflict, message: "Intercom rejected the request (HTTP 409) [conflict]",
		},
		{
			name: "plain text", status: http.StatusBadRequest, body: `SENTINEL bad request`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Intercom rejected the request (HTTP 400)",
		},
		{
			name: "reflected token", status: http.StatusBadRequest, body: `{"type":"error.list","errors":[{"code":"` + testAccessToken + `","field":"token"}]}`,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureValidation, message: "Intercom rejected the request (HTTP 400) [fields: token]",
		},
		{
			name: "redirect", status: http.StatusFound, body: ``,
			branch: intercom.GetConversationBranchProviderRejected, kind: sdkgo.FailureProtocol,
			message: "Intercom redirected the request (HTTP 302); check the region configuration",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://app.intercom.com/")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newTestDexContext("failure-"+test.name), newIntercomClient(t, provider.URL).GetConversation(),
				intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
			require.NoError(t, err, "a conclusive failure is a branch, not a retry")
			require.Equal(t, test.branch, result.Branch)
			require.NotNil(t, result.Failure)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.False(t, containsAny(result.Failure.Message, "SENTINEL", testAccessToken))
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
		})
	}
}

func TestRequestIDFromAnErrorBodyReachesTheReceipt(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"type":"error.list","request_id":"f93ecfa8-d08a-4325-8694-89aeb89c8f85","errors":[{"code":"not_found"}]}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("request-id"), newIntercomClient(t, provider.URL).GetConversation(),
		intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
	require.NoError(t, err)
	require.Equal(t, "f93ecfa8-d08a-4325-8694-89aeb89c8f85", result.Receipt.ProviderRequestID)
	require.Equal(t, testConversation, result.Receipt.ProviderObjectID)
	require.Equal(t, "intercom", result.Receipt.Provider)
}

func TestRateLimitsWaitForTheResetAndServerErrorsRetry(t *testing.T) {
	now := time.Unix(1767225600, 0)
	for _, test := range []struct {
		name          string
		status        int
		header        http.Header
		expectedDelay time.Duration
	}{
		{name: "rate limit reset", status: http.StatusTooManyRequests, header: http.Header{"X-Ratelimit-Reset": {unixText(now.Add(7 * time.Second))}}, expectedDelay: 7 * time.Second},
		{name: "rate limit reset already passed", status: http.StatusTooManyRequests, header: http.Header{"X-Ratelimit-Reset": {unixText(now.Add(-time.Second))}}, expectedDelay: time.Second},
		{name: "rate limit reset far away", status: http.StatusTooManyRequests, header: http.Header{"X-Ratelimit-Reset": {unixText(now.Add(time.Hour))}}, expectedDelay: time.Minute},
		{name: "retry after", status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"3"}}, expectedDelay: 3 * time.Second},
		{name: "server error", status: http.StatusServiceUnavailable},
		{name: "timeout", status: http.StatusRequestTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				for name, values := range test.header {
					response.Header()[name] = values
				}
				writeJSON(t, response, test.status, `{"type":"error.list","errors":[{"code":"rate_limit_exceeded","message":"SENTINEL"}]}`)
			})
			client := newIntercomClient(t, provider.URL, intercom.WithClock(func() time.Time { return now }))
			_, err := sdkgo.RunQuery(newTestDexContext("retry-"+test.name), client.GetConversation(), intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
			require.Error(t, err)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.NotContains(t, err.Error(), "SENTINEL")
			var retryAfter *dex.RetryAfterError
			if test.expectedDelay == 0 {
				require.False(t, errors.As(err, &retryAfter), "the Step retry policy chooses the delay")
				return
			}
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, test.expectedDelay, retryAfter.After)
		})
	}
}

func TestOversizedMalformedAndCredentialReflectingResponsesAreInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":            `{"type":"conversation","id":"` + testConversation + `","padding":"` + strings.Repeat("x", 2048) + `"}`,
		"malformed":            `{"type":"conversation",`,
		"another conversation": conversationJSON(t, "999", "open", nil),
		"reflected token":      `{"type":"conversation","id":"` + testConversation + `","title":"` + testAccessToken + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client, err := intercom.New(intercom.Config{MaxResponseBytes: 1024}, testCredentialProvider(), intercom.WithAPIBaseURL(provider.URL))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newTestDexContext("invalid-"+name), client.GetConversation(), intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
			require.NoError(t, err)
			require.Equal(t, intercom.GetConversationBranchInvalidResponse, result.Branch)
			require.NotContains(t, result.Failure.Message, testAccessToken)
		})
	}
}

func TestTransportFailuresRetryWithoutAResponse(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		hijacker, isHijacker := response.(http.Hijacker)
		require.True(t, isHijacker)
		connection, _, err := hijacker.Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	_, err := sdkgo.RunQuery(newTestDexContext("transport"), newIntercomClient(t, provider.URL).GetConversation(), intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Equal(t, "Intercom request failed before a response arrived", retry.Failure.Message)
}

func TestInvalidCredentialsSelectDefectBeforeAnyRequest(t *testing.T) {
	provider := newRecordingIntercom(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	for name, token := range map[string]string{"missing": "", "with space": "token with space", "with newline": "token\nInjected: yes"} {
		client, err := intercom.New(intercom.Config{}, sdkgo.StaticCredentialProvider[intercom.Credentials]{
			intercomConnection: {AccessToken: sdkgo.NewSecretString(token)},
		}, intercom.WithAPIBaseURL(provider.URL))
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newTestDexContext("credentials-"+name), client.GetConversation(), intercomConnection, intercom.GetConversationInput{ConversationID: testConversation})
		require.NoError(t, err)
		require.Equal(t, intercom.GetConversationBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	}
	require.Zero(t, provider.requestCount())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
