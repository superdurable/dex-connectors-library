// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewRejectsInsecureEndpointsAndInvalidLimits(t *testing.T) {
	credentials := staticCredentials()
	_, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: "http://admin.example.com"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
	_, err = workspaceadmin.New(workspaceadmin.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = workspaceadmin.New(workspaceadmin.Config{ListUsersPageSize: 501}, credentials)
	require.ErrorContains(t, err, "from 1 to 500")
	_, err = workspaceadmin.New(workspaceadmin.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = workspaceadmin.New(workspaceadmin.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := workspaceadmin.New(workspaceadmin.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestUnauthorizedRequestRefreshesOnceAndResendsWithTheReplacement(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.seedUser("ada@example.com", nil)
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: directory.URL}, credentials)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("refresh-once"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchFound, result.Branch)
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Len(t, directory.recorded(), 1, "the rejected request never reaches the stateful fake")
}

func TestRetryableGoogleResponsesReturnRetryWithTheProviderDelay(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		reason     string
		retryAfter string
		kind       sdkgo.FailureKind
		delay      time.Duration
	}{
		{name: "per-user rate limit", status: http.StatusForbidden, reason: "userRateLimitExceeded", retryAfter: "7", kind: sdkgo.FailureRateLimit, delay: 7 * time.Second},
		{name: "concurrent request quota", status: http.StatusForbidden, reason: "quotaExceeded", kind: sdkgo.FailureRateLimit},
		{name: "account rate limit", status: http.StatusTooManyRequests, reason: "rateLimitExceeded", kind: sdkgo.FailureRateLimit},
		{name: "backend error", status: http.StatusServiceUnavailable, reason: "backendError", kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := newFakeDirectory(t)
			directory.retryAfter = test.retryAfter
			directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
				return test.status, googleError(test.status, test.reason), true
			})
			client := newTestClient(t, directory.URL)

			_, err := sdkgo.RunQuery(newTestDexContext("retryable"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
			failure := requireRetry(t, err)
			require.Equal(t, test.kind, failure.Kind)
			require.Contains(t, failure.Message, "("+test.reason+")")
			var retryAfter *dex.RetryAfterError
			if test.delay == 0 {
				require.False(t, errors.As(err, &retryAfter), "no Retry-After header uses the Step retry policy")
				return
			}
			require.True(t, errors.As(err, &retryAfter))
			require.Equal(t, test.delay, retryAfter.After)
		})
	}
}

func TestUserCreationIncompleteIsRetriedWithoutRepeatingGoogleText(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.seedGroup("staff@example.com")
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusPreconditionFailed, map[string]any{"error": map[string]any{
			"code": 412, "message": "User creation is not complete. " + providerMessageSentinel,
			"errors": []any{map[string]any{"reason": "conditionNotMet", "message": "User creation is not complete."}},
		}}, true
	})
	client := newTestClient(t, directory.URL)

	_, err := sdkgo.RunMutation(newTestDexContext("propagation"), client.AddUserToGroup(), testConnection,
		workspaceadmin.AddUserToGroupInput{GroupKey: "staff@example.com", MemberEmail: "ada@example.com"})
	failure := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureAvailability, failure.Kind)
	require.Equal(t, "Google is still creating the account", failure.Message)
}

func TestFailuresNeverRepeatProviderMessagesOrTokens(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusForbidden, map[string]any{"error": map[string]any{
			"code": 403, "message": providerMessageSentinel + " " + testAccessToken,
			"errors": []any{map[string]any{"reason": "forbidden", "message": providerMessageSentinel}},
		}}, true
	})
	client := newTestClient(t, directory.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("safe-failure"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Equal(t, "provider rejected the request with HTTP 403 (forbidden)", result.Failure.Message)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestOversizedResponsesSelectInvalidResponse(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.seedUser("ada@example.com", nil)
	client, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: directory.URL, MaxResponseBytes: 64}, staticCredentials())
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("oversized"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestRedirectsAreNotFollowedWithTheCredential(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusFound, nil, true
	})
	client := newTestClient(t, directory.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("redirect"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchProviderRejected, result.Branch)
	require.Len(t, directory.recorded(), 1)
}

func TestUnavailableCredentialsSelectDefectWithoutARequest(t *testing.T) {
	directory := newFakeDirectory(t)
	client, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: directory.URL}, sdkgo.StaticCredentialProvider[workspaceadmin.Credentials]{})
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("no-credentials"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, directory.recorded())
}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (workspaceadmin.Credentials, error) {
	return workspaceadmin.Credentials{AuthMethodID: workspaceadmin.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

// ResolveWithRefresh returns the stored credential unchanged: its expiry has not passed.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[workspaceadmin.Credentials],
) (workspaceadmin.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[workspaceadmin.Credentials],
) (workspaceadmin.Credentials, error) {
	provider.forcedRefreshes++
	return workspaceadmin.Credentials{AuthMethodID: workspaceadmin.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)}, nil
}
