// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEveryRequestIsOneAuthenticatedVersionedGraphQLPost(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"items": []any{itemJSON(testItemID, "Fire drill")}})
	})
	result, err := sdkgo.RunQuery(newMondayDexContext("get"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.NoError(t, err)
	require.Equal(t, monday.GetItemBranchFound, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method, "monday.com removed GraphQL over GET in 2025-01")
	require.Equal(t, "/v2", request.path)
	require.Equal(t, testAPIToken, request.header.Get("Authorization"), "monday.com documents the raw token without a Bearer prefix")
	require.Equal(t, monday.APIVersion, request.header.Get("API-Version"))
	require.Equal(t, "2026-10", monday.APIVersion)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Empty(t, request.header.Get("Idempotency-Key"), "monday.com ignores the key on queries")
	require.Equal(t, "request-0001", result.Receipt.ProviderRequestID)
	require.Equal(t, testItemID, result.Receipt.ProviderObjectID)
}

func TestMutationsSendTheStepIdempotencyKeyOnEveryAttempt(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{"message":"SENTINEL","extensions":{"code":"INTERNAL_SERVER_ERROR"}}]}`)
			return
		}
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
	})
	client := newMondayClient(t, provider.URL)
	ctx := newMondayDexContext("create-keyed")
	_, err := sdkgo.RunMutation(ctx, client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a 5xx is retried, because the key replays an applied create")
	result, err := sdkgo.RunMutation(ctx, client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	first, second := provider.request(0).header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key")
	require.NotEmpty(t, first)
	require.Equal(t, first, second, "a retry of one Step execution reuses its key")
	require.Equal(t, string(result.Receipt.CallID), first, "the key is the stable Call ID, a UUID as monday.com recommends")

	_, err = sdkgo.RunMutation(newMondayDexContext("create-other-step"), client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.NotEqual(t, first, provider.request(2).header.Get("Idempotency-Key"), "another Step execution is another intent")
}

func TestGraphQLErrorsMapOntoBranchesWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		readBranch sdkgo.BranchID
		kind       sdkgo.FailureKind
		message    string
	}{
		{name: "column value", status: 200, body: `{"data":{"items":null},"errors":[{"message":"SENTINEL","extensions":{"code":"ColumnValueException","status_code":200,"error_data":{"column_id":"status"}}}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureValidation, message: "monday.com rejected the input (HTTP 200) [ColumnValueException; column status]"},
		{name: "user unauthorized in a 200", status: 200, body: `{"data":[],"errors":[{"message":"SENTINEL","extensions":{"code":"UserUnauthorizedException","error_data":{},"status_code":403}}],"account_id":1}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureAuthorization, message: "monday.com denied permission (HTTP 200) [UserUnauthorizedException]"},
		{name: "missing scope", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"missingRequiredPermissions"}}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "not authenticated", status: 401, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"NOT_AUTHENTICATED"}}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{name: "ip restricted without a code", status: 401, body: `{"errors":[{"message":"Your ip is restricted"}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{name: "resource not found", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"ResourceNotFoundException","status_code":404}}]}`,
			readBranch: monday.GetItemBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "invalid board", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"InvalidBoardIdException"}}]}`,
			readBranch: monday.GetItemBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "daily limit", status: 429, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"DAILY_LIMIT_EXCEEDED"}}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureQuotaExhausted, message: "monday.com daily call limit is exhausted until midnight UTC (HTTP 429) [DAILY_LIMIT_EXCEEDED]"},
		{name: "parse error", status: 200, body: `{"errors":[{"message":"Parse error on \"}\" SENTINEL","extensions":{"code":"GRAPHQL_PARSE_FAILED"}}]}`,
			readBranch: monday.GetItemBranchDefect, kind: sdkgo.FailureProtocol},
		{name: "bad request", status: 400, body: `{"errors":[{"message":"SENTINEL"}]}`,
			readBranch: monday.GetItemBranchDefect, kind: sdkgo.FailureProtocol},
		{name: "unknown code", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"SomethingNewException"}}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "message-only code", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"has spaces SENTINEL"}}]}`,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureProviderRejection, message: "monday.com rejected the request (HTTP 200)"},
		{name: "reflected token", status: 200, body: `{"data":{"items":[]},"extensions":{"echo":"` + testAPIToken + `"}}`,
			readBranch: monday.GetItemBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "not JSON", status: 200, body: `<html>SENTINEL</html>`,
			readBranch: monday.GetItemBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "redirect", status: 302, body: ``,
			readBranch: monday.GetItemBranchProviderRejected, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				if test.status == 302 {
					response.Header().Set("Location", "https://elsewhere.example/v2")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newMondayDexContext("errors-"+test.name), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
			require.NoError(t, err, "a conclusive answer is a branch, not a retry")
			require.Equal(t, test.readBranch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			if test.message != "" {
				require.Equal(t, test.message, result.Failure.Message)
			}
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
		})
	}
}

func TestRateLimitsLocksAndOutagesAreRetriedAfterMondaysDelay(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		body       string
		kind       sdkgo.FailureKind
		delay      time.Duration
	}{
		{name: "complexity budget in extensions", status: 429, body: `{"errors":[{"message":"Complexity budget exhausted","extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED","complexity":6182,"complexity_budget_left":100,"complexity_budget_limit":5000000,"retry_in_seconds":37,"status_code":429}}]}`,
			kind: sdkgo.FailureRateLimit, delay: 37 * time.Second},
		{name: "complexity in a 200", status: 200, body: `{"errors":[{"message":"x","extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED","retry_in_seconds":12,"status_code":429}}]}`,
			kind: sdkgo.FailureRateLimit, delay: 12 * time.Second},
		{name: "minute limit with Retry-After", status: 429, retryAfter: "21", body: `{"errors":[{"message":"Rate Limit Exceeded"}]}`,
			kind: sdkgo.FailureRateLimit, delay: 21 * time.Second},
		{name: "concurrency", status: 429, body: `{"errors":[{"message":"x","extensions":{"code":"maxConcurrencyExceeded","retry_in_seconds":2}}]}`,
			kind: sdkgo.FailureRateLimit, delay: 2 * time.Second},
		{name: "temporarily blocked", status: 200, body: `{"errors":[{"message":"x","extensions":{"code":"API_TEMPORARILY_BLOCKED"}}]}`,
			kind: sdkgo.FailureAvailability},
		{name: "board locked", status: 423, retryAfter: "3", body: `{"errors":[{"message":"Resource is currently locked, please try again later"}]}`,
			kind: sdkgo.FailureAvailability, delay: 3 * time.Second},
		{name: "server error", status: 500, body: `{"errors":[{"message":"Internal Server Error"}]}`, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", status: 504, body: ``, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			_, err := sdkgo.RunQuery(newMondayDexContext("retry-"+test.name), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			var retryAfter *dex.RetryAfterError
			if test.delay > 0 {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			} else {
				require.False(t, errorsAsRetryAfter(err), "no delay falls back to the Step retry policy")
			}
		})
	}
}

func TestTransportFailuresAreRetried(t *testing.T) {
	client := newMondayClient(t, closedLoopbackURL(t))
	_, err := sdkgo.RunQuery(newMondayDexContext("refused"), client.GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, "monday.com could not be reached; no request was sent", retry.Failure.Message)

	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { dropConnection(t, response) })
	_, err = sdkgo.RunQuery(newMondayDexContext("dropped"), newMondayClient(t, provider.URL).GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestOversizedResponsesAreInvalid(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"items":[{"id":"`+testItemID+`","name":"`+strings.Repeat("x", 2048)+`"}]}}`)
	})
	client, err := monday.New(monday.Config{MaxResponseBytes: 1024}, testCredentialProvider(), monday.WithAPIURL(provider.URL+"/v2"))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newMondayDexContext("oversized"), client.GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.NoError(t, err)
	require.Equal(t, monday.GetItemBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	_, err := monday.New(monday.Config{MaxResponseBytes: -1}, testCredentialProvider())
	require.Error(t, err)
	_, err = monday.New(monday.Config{}, nil)
	require.Error(t, err)
	_, err = monday.New(monday.Config{}, testCredentialProvider(), monday.WithAPIURL("http://api.example.com/v2"))
	require.ErrorContains(t, err, "HTTPS")
	_, err = monday.New(monday.Config{}, testCredentialProvider(), monday.WithAPIURL("https://api.example.com/v2?token=x"))
	require.Error(t, err)
	_, err = monday.New(monday.Config{}, testCredentialProvider(), nil)
	require.Error(t, err)
}

func TestInvalidCredentialsSelectDefectWithoutARequest(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	for name, providerCredentials := range map[string]sdkgo.StaticCredentialProvider[monday.Credentials]{
		"token with a space":   {mondayConnection: {AuthMethodID: monday.PersonalAPITokenAuthMethodID, APIToken: sdkgo.NewSecretString("two words")}},
		"unknown auth method":  {mondayConnection: {AuthMethodID: "basic", APIToken: sdkgo.NewSecretString(testAPIToken)}},
		"blank oauth token":    {mondayConnection: {AuthMethodID: monday.OAuthAuthMethodID}},
		"connection not found": {},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := monday.New(monday.Config{}, providerCredentials, monday.WithAPIURL(provider.URL+"/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newMondayDexContext("credentials"), client.GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
			require.NoError(t, err)
			require.Equal(t, monday.GetItemBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
		})
	}
}

func errorsAsRetryAfter(err error) bool {
	var retryAfter *dex.RetryAfterError
	return errors.As(err, &retryAfter)
}
