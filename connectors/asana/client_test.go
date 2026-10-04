// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewRejectsInvalidEndpointsAndLimits(t *testing.T) {
	credentials := staticAsanaCredentials()
	_, err := asana.New(asana.Config{Endpoint: "http://app.asana.com/api/1.0"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = asana.New(asana.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = asana.New(asana.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = asana.New(asana.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := asana.New(asana.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestRequestsUseTheAPIBaseAndTheBearerToken(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":`+taskJSON(testTaskID, "Replace badge reader", false, testSectionID)+`}`)
	})
	client := newAsanaClient(t, provider.URL+"/api/1.0")

	result, err := sdkgo.RunQuery(newAsanaDexContext("base-path"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, asana.GetTaskBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/api/1.0/tasks/"+testTaskID, request.path)
	require.Equal(t, "Bearer "+testAccessToken, request.authorization)
	require.Equal(t, testTaskID, result.Receipt.ProviderObjectID)
	require.Empty(t, result.Receipt.ProviderRequestID, "Asana documents no request ID header")
}

func TestUnauthorizedTokenIsRejectedWithoutAResend(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"errors":[{"message":"SENTINEL Not Authorized"}]}`)
	})
	client := newAsanaClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newAsanaDexContext("unauthorized"), client.CreateTask(), asanaConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchProviderRejected, result.Branch, "a 401 proves Asana created nothing")
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 1, provider.requestCount(), "a personal access token cannot be refreshed, so nothing is resent")
	requireNoSentinel(t, result)
}

func TestFailuresNameTheStatusButNeverProviderMessages(t *testing.T) {
	for _, test := range []struct {
		status int
		kind   sdkgo.FailureKind
	}{
		{http.StatusBadRequest, sdkgo.FailureValidation},
		{http.StatusPaymentRequired, sdkgo.FailureAuthorization},
		{http.StatusForbidden, sdkgo.FailureAuthorization},
		{http.StatusUnavailableForLegalReasons, sdkgo.FailureProviderRejection},
	} {
		provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, test.status, `{"errors":[{"message":"assignee: SENTINEL owner@example.com is not a member","help":"SENTINEL"}]}`)
		})
		client := newAsanaClient(t, provider.URL)
		result, err := sdkgo.RunQuery(newAsanaDexContext("safe-failure"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
		require.NoError(t, err)
		require.Equal(t, asana.GetTaskBranchProviderRejected, result.Branch)
		require.Equal(t, test.kind, result.Failure.Kind)
		require.Equal(t, fmt.Sprintf("Asana rejected the task with HTTP %d", test.status), result.Failure.Message)
		requireNoSentinel(t, result)
	}
}

func TestResponseThatReflectsTheAccessTokenIsNeverReturned(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":`+taskJSON(testTaskID, "token "+testAccessToken, false, testSectionID)+`}`)
	})
	client := newAsanaClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newAsanaDexContext("reflection"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, asana.GetTaskBranchInvalidResponse, result.Branch)
	requireNoSentinel(t, result)
}

func TestRateLimitedReadRetriesAfterRetryAfter(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "7")
		writeJSON(t, response, http.StatusTooManyRequests, `{"errors":[{"message":"SENTINEL You have made too many requests recently."}]}`)
	})
	client := newAsanaClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newAsanaDexContext("rate-limit"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
	requireRetry(t, err, sdkgo.FailureRateLimit)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

func TestUnavailableCredentialsSelectDefectWithoutAProviderRequest(t *testing.T) {
	provider := newRecordingAsana(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, credentials := range map[string]sdkgo.StaticCredentialProvider[asana.Credentials]{
		"missing connection":  {},
		"header-unsafe token": {asanaConnection: {AccessToken: sdkgo.NewSecretString("token\r\nX-Injected: 1")}},
	} {
		client, err := asana.New(asana.Config{Endpoint: provider.URL}, credentials)
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(newAsanaDexContext("no-credentials"), client.CreateTask(), asanaConnection, validCreateTaskInput())
		require.NoError(t, err, name)
		require.Equal(t, asana.CreateTaskBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind, name)
	}
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		http.Redirect(response, request, "https://attacker.example/steal", http.StatusFound)
	})
	client := newAsanaClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newAsanaDexContext("redirect"), client.GetTask(), asanaConnection, asana.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, asana.GetTaskBranchInvalidResponse, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}
