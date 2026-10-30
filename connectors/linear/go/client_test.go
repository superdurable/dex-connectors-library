// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestPersonalAPIKeyIsSentRawAndEveryRequestIsANamedGraphQLPost(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{}}})
	})
	result, err := runQuery("get", newAPIKeyClient(t, provider.URL).GetIssue(), linear.GetIssueInput{IssueID: "eng-42"})
	require.NoError(t, err)
	require.Equal(t, linear.GetIssueBranchNotFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/graphql", request.path)
	require.Equal(t, testAPIKey, request.header.Get("Authorization"), "Linear rejects an API key behind Bearer")
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Equal(t, "LinearGetIssue", request.operationName)
	require.True(t, strings.HasPrefix(request.query, "query LinearGetIssue("))
	require.Equal(t, map[string]any{"filter": map[string]any{
		"team": map[string]any{"key": map[string]any{"eq": "ENG"}}, "number": map[string]any{"eq": float64(42)},
	}}, request.variables, "input travels only as variables")
	require.Equal(t, "request-0001", result.Receipt.ProviderRequestID)
}

func TestOAuthAccessTokenIsSentAsBearer(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"users": map[string]any{"nodes": []any{}}})
	})
	credentials := sdkgo.StaticCredentialProvider[linear.Credentials]{linearConnection: {
		AuthMethodID: linear.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken),
		OAuthClientID: "client", OAuthClientSecret: sdkgo.NewSecretString("secret"), RefreshToken: sdkgo.NewSecretString("refresh"),
	}}
	result, err := runQuery("find", newLinearClient(t, provider.URL, credentials).FindUserByEmail(), linear.FindUserByEmailInput{Email: "alice@example.com"})
	require.NoError(t, err)
	require.Equal(t, linear.FindUserByEmailBranchNotFound, result.Branch)
	require.Equal(t, "Bearer "+testAccessToken, provider.request(0).header.Get("Authorization"))
}

// TestErrorsMapToBranchesWithoutLinearText covers Linear's extensions.code and extensions.type values and
// the HTTP statuses that carry them; Linear answers a rate limit with HTTP 400.
func TestErrorsMapToBranchesWithoutLinearText(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		retryAfter  string
		branch      sdkgo.BranchID
		isRetry     bool
		kind        sdkgo.FailureKind
		delay       time.Duration
		messagePart string
	}{
		{name: "rate limit on HTTP 400", status: 400, body: linearError("RATELIMITED", "ratelimited"), isRetry: true, kind: sdkgo.FailureRateLimit, messagePart: "[RATELIMITED; ratelimited]"},
		{name: "rate limit with Retry-After", status: 429, body: linearError("RATELIMITED", "ratelimited"), retryAfter: "7", isRetry: true, kind: sdkgo.FailureRateLimit, delay: 7 * time.Second},
		{name: "authentication", status: 401, body: linearError("AUTHENTICATION_ERROR", "authentication error"), branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{name: "forbidden", status: 403, body: linearError("FORBIDDEN", "forbidden"), branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "feature not accessible", status: 200, body: linearError("FEATURE_NOT_ACCESSIBLE", "feature not accessible"), branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "usage limit", status: 200, body: linearError("USAGE_LIMIT_EXCEEDED", "usage limit exceeded"), branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureQuotaExhausted},
		{name: "input error", status: 400, body: linearError("INPUT_ERROR", "invalid input"), branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "graphql validation", status: 400, body: linearError("GRAPHQL_VALIDATION_FAILED", "graphql error"), branch: linear.GetIssueBranchDefect, kind: sdkgo.FailureProtocol},
		{name: "lock timeout", status: 200, body: linearError("LOCK_TIMEOUT", "lock timeout"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "internal error", status: 500, body: linearError("INTERNAL_SERVER_ERROR", "internal error"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "bare 502", status: 502, body: "<html>bad gateway</html>", isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "bare 400", status: 400, body: `{}`, branch: linear.GetIssueBranchDefect, kind: sdkgo.FailureProtocol},
		{name: "redirect", status: 302, body: ``, branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureProtocol},
		{name: "unknown code", status: 200, body: linearError("SOMETHING_NEW", "unrecognized"), branch: linear.GetIssueBranchProviderRejected, kind: sdkgo.FailureProviderRejection, messagePart: "[SOMETHING_NEW]"},
		{name: "malformed 2xx", status: 200, body: `not json`, branch: linear.GetIssueBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "reflected key", status: 200, body: `{"data":{"issues":{"nodes":[]}},"echo":"` + testAPIKey + `"}`, branch: linear.GetIssueBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				if testCase.retryAfter != "" {
					response.Header().Set("Retry-After", testCase.retryAfter)
				}
				if testCase.status == http.StatusFound {
					response.Header().Set("Location", "https://evil.example/graphql")
				}
				writeJSON(t, response, testCase.status, testCase.body)
			})
			result, err := runQuery("errors-"+testCase.name, newAPIKeyClient(t, provider.URL).GetIssue(), linear.GetIssueInput{IssueID: testIssueID})
			require.Equal(t, 1, provider.requestCount(), "no redirect is followed and nothing is resent")
			if testCase.isRetry {
				retry := requireRetry(t, err, testCase.kind, testCase.delay)
				require.Contains(t, retry.Failure.Message, testCase.messagePart)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Contains(t, result.Failure.Message, testCase.messagePart)
		})
	}
}

func TestTransportFailuresAreRetried(t *testing.T) {
	_, err := runQuery("refused", newAPIKeyClient(t, closedLoopbackURL(t)).GetIssue(), linear.GetIssueInput{IssueID: testIssueID})
	retry := requireRetry(t, err, sdkgo.FailureTransport, 0)
	require.Equal(t, "Linear could not be reached; no request was sent", retry.Failure.Message)
}

func TestOversizedResponseSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"issues":{"nodes":[]}},"padding":"`+strings.Repeat("x", 2048)+`"}`)
	})
	client, err := linear.New(linear.Config{MaxResponseBytes: 1024}, apiKeyCredentials(), linear.WithAPIURL(provider.URL+"/graphql"))
	require.NoError(t, err)
	result, err := runQuery("oversized", client.SearchIssues(), linear.SearchIssuesInput{})
	require.NoError(t, err)
	require.Equal(t, linear.SearchIssuesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	_, err := linear.New(linear.Config{}, apiKeyCredentials(), linear.WithAPIURL("http://linear.example/graphql"))
	require.ErrorContains(t, err, "HTTPS")
	_, err = linear.New(linear.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = linear.New(linear.Config{WebhookSignatureTolerance: time.Millisecond}, apiKeyCredentials())
	require.ErrorContains(t, err, "at least one second")
	_, err = linear.New(linear.Config{MaxResponseBytes: -1}, apiKeyCredentials())
	require.Error(t, err)
}

func TestUnusableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	credentials := sdkgo.StaticCredentialProvider[linear.Credentials]{linearConnection: {
		AuthMethodID: linear.PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString("key with spaces"),
	}}
	result, err := runQuery("unusable", newLinearClient(t, provider.URL, credentials).GetIssue(), linear.GetIssueInput{IssueID: testIssueID})
	require.NoError(t, err)
	require.Equal(t, linear.GetIssueBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "key with spaces")
	require.Zero(t, provider.requestCount())
}
