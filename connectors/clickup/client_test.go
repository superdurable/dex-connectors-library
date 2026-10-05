// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestRequestsSendTheRawPersonalTokenToTheAPIHost(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: testTaskID, name: "Fix login"}))
	})
	result, err := sdkgo.RunQuery(newTestDexContext("get"), newClickUpClient(t, provider.URL).GetTask(), clickupConnection, clickup.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, clickup.GetTaskBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, testAPIToken, request.header.Get("Authorization"), "ClickUp personal tokens are sent without a Bearer scheme")
	require.Equal(t, "application/json", request.header.Get("Accept"))
	require.Equal(t, "https://api.clickup.com/api/v2", clickup.APIBaseURL)
}

func TestFailureStatusesMapToBranchesWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
		expectedText   string
	}{
		{name: "not found", status: http.StatusNotFound, body: clickupError("ITEM_013"), expectedBranch: clickup.GetTaskBranchNotFound,
			expectedKind: sdkgo.FailureNotFound, expectedText: "ClickUp found no such resource (HTTP 404) [ITEM_013]"},
		{name: "team not authorized", status: http.StatusUnauthorized, body: clickupError("OAUTH_027"), expectedBranch: clickup.GetTaskBranchNotFound,
			expectedKind: sdkgo.FailureAuthorization, expectedText: "ClickUp reports that the resource's Workspace is not authorized for the token (HTTP 401) [OAUTH_027]"},
		{name: "token not found", status: http.StatusUnauthorized, body: clickupError("OAUTH_019"), expectedBranch: clickup.GetTaskBranchProviderRejected,
			expectedKind: sdkgo.FailureAuthentication, expectedText: "ClickUp rejected the personal API token (HTTP 401) [OAUTH_019]"},
		{name: "validation", status: http.StatusBadRequest, body: clickupError("INPUT_005"), expectedBranch: clickup.GetTaskBranchProviderRejected,
			expectedKind: sdkgo.FailureValidation, expectedText: "ClickUp rejected the request (HTTP 400) [INPUT_005]"},
		{name: "unsafe error code", status: http.StatusForbidden, body: `{"err":"x","ECODE":"` + providerSentinel + `"}`, expectedBranch: clickup.GetTaskBranchProviderRejected,
			expectedKind: sdkgo.FailureAuthorization, expectedText: "ClickUp rejected the request (HTTP 403)"},
		{name: "redirect", status: http.StatusFound, body: "", expectedBranch: clickup.GetTaskBranchProviderRejected,
			expectedKind: sdkgo.FailureProtocol, expectedText: "ClickUp redirected the request (HTTP 302)"},
		{name: "credential reflected", status: http.StatusOK, body: `{"id":"` + testTaskID + `","name":"` + testAPIToken + `"}`, expectedBranch: clickup.GetTaskBranchInvalidResponse,
			expectedKind: sdkgo.FailureProtocol, expectedText: "ClickUp response reflected the connection credential"},
		{name: "malformed", status: http.StatusOK, body: `[]`, expectedBranch: clickup.GetTaskBranchInvalidResponse,
			expectedKind: sdkgo.FailureProtocol, expectedText: "ClickUp returned an invalid task: task is not a JSON object"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://elsewhere.example.com/")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newTestDexContext("failure-"+test.name), newClickUpClient(t, provider.URL).GetTask(), clickupConnection, clickup.GetTaskInput{TaskID: testTaskID})
			require.NoError(t, err, "a conclusive failure is a branch, not a retry")
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			require.Equal(t, test.expectedText, result.Failure.Message)
			require.NotContains(t, result.Failure.Message, testAPIToken)
			require.Equal(t, 1, provider.requestCount(), "a redirect is never followed")
		})
	}
}

func TestOversizedResponseSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"`+testTaskID+`","name":"`+strings.Repeat("x", 512)+`"}`)
	})
	client, err := clickup.New(clickup.Config{MaxResponseBytes: 256}, testCredentialProvider(), clickup.WithAPIBaseURL(provider.URL))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newTestDexContext("oversized"), client.GetTask(), clickupConnection, clickup.GetTaskInput{TaskID: testTaskID})
	require.NoError(t, err)
	require.Equal(t, clickup.GetTaskBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestRateLimitsWaitForTheResetAndServerErrorsRetry(t *testing.T) {
	now := time.Unix(1767225600, 0)
	for _, test := range []struct {
		name          string
		status        int
		header        http.Header
		expectedDelay time.Duration
		expectedKind  sdkgo.FailureKind
	}{
		{name: "reset", status: http.StatusTooManyRequests, header: http.Header{"X-Ratelimit-Reset": {strconv.FormatInt(now.Add(17*time.Second).Unix(), 10)}},
			expectedDelay: 17 * time.Second, expectedKind: sdkgo.FailureRateLimit},
		{name: "reset beyond a minute", status: http.StatusTooManyRequests, header: http.Header{"X-Ratelimit-Reset": {strconv.FormatInt(now.Add(time.Hour).Unix(), 10)}},
			expectedDelay: time.Minute, expectedKind: sdkgo.FailureRateLimit},
		{name: "reset passed", status: http.StatusTooManyRequests, header: http.Header{"X-Ratelimit-Reset": {strconv.FormatInt(now.Add(-time.Second).Unix(), 10)}},
			expectedDelay: time.Second, expectedKind: sdkgo.FailureRateLimit},
		{name: "server error", status: http.StatusBadGateway, expectedKind: sdkgo.FailureAvailability},
		{name: "request timeout", status: http.StatusRequestTimeout, expectedKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				for name, values := range test.header {
					response.Header()[name] = values
				}
				writeJSON(t, response, test.status, clickupError("RATE_001"))
			})
			client := newClickUpClient(t, provider.URL, clickup.WithClock(func() time.Time { return now }))
			_, err := sdkgo.RunQuery(newTestDexContext("retry-"+test.name), client.GetTask(), clickupConnection, clickup.GetTaskInput{TaskID: testTaskID})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.expectedKind, retry.Failure.Kind)
			require.NotContains(t, retry.Failure.Message, providerSentinel)
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

func TestUnusableTokensSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	for name, token := range map[string]string{
		"oauth access token": "4411723_abcdefABCDEF0123456789",
		"bare prefix":        "pk_",
		"space":              "pk_4411723 TOKEN",
		"blank":              "",
	} {
		t.Run(name, func(t *testing.T) {
			client, err := clickup.New(clickup.Config{}, sdkgo.StaticCredentialProvider[clickup.Credentials]{
				clickupConnection: {APIToken: sdkgo.NewSecretString(token)},
			}, clickup.WithAPIBaseURL(provider.URL))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newTestDexContext("token-"+name), client.GetTask(), clickupConnection, clickup.GetTaskInput{TaskID: testTaskID})
			require.NoError(t, err)
			require.Equal(t, clickup.GetTaskBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			if len(token) > len(clickup.PersonalAPITokenPrefix) {
				require.NotContains(t, result.Failure.Message, token)
			}
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	_, err := clickup.New(clickup.Config{}, testCredentialProvider(), clickup.WithAPIBaseURL("http://api.example.com"))
	require.ErrorContains(t, err, "ClickUp API base URL")
	_, err = clickup.New(clickup.Config{MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	_, err = clickup.New(clickup.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = clickup.New(clickup.Config{}, testCredentialProvider(), nil)
	require.ErrorContains(t, err, "option is nil")
}
