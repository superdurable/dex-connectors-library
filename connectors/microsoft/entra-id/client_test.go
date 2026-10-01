// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewRejectsInvalidLimitsAndNonLoopbackLocalProviders(t *testing.T) {
	credentials := staticCredentials()
	_, err := entraid.New(entraid.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = entraid.New(entraid.Config{ListUsersPageSize: 1000}, credentials)
	require.ErrorContains(t, err, "from 1 to 999")
	_, err = entraid.New(entraid.Config{CreationKeyAttribute: "extensionAttribute16"}, credentials)
	require.ErrorContains(t, err, "creationKeyAttribute")
	_, err = entraid.New(entraid.Config{}, credentials, entraid.WithLocalProviderURL("https://graph.example.com"))
	require.ErrorContains(t, err, "loopback")
	_, err = entraid.New(entraid.Config{}, credentials, entraid.WithLocalProviderURL("http://127.0.0.1:8930/v1.0"))
	require.ErrorContains(t, err, "path")
	_, err = entraid.New(entraid.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = entraid.New(entraid.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	client, err := entraid.New(entraid.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestUnauthorizedRequestRefreshesOnceAndResendsWithTheReplacement(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	credentials := &rejectionRefreshingCredentialProvider{}
	client, err := entraid.New(entraid.Config{}, credentials, entraid.WithLocalProviderURL(graph.URL))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("refresh-once"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: userID})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchFound, result.Branch)
	require.Equal(t, 1, credentials.forcedRefreshes)
	require.Len(t, graph.Requests(), 2, "one rejected request, one resend")
}

func TestRetryableGraphResponsesReturnRetryWithTheProviderDelay(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		code   string
		kind   sdkgo.FailureKind
	}{
		{name: "throttled", status: http.StatusTooManyRequests, code: "TooManyRequests", kind: sdkgo.FailureRateLimit},
		{name: "unavailable", status: http.StatusServiceUnavailable, code: "serviceNotAvailable", kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, code: "generalException", kind: sdkgo.FailureAvailability},
		{name: "concurrent change", status: http.StatusConflict, code: "Directory_ConcurrencyViolation", kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph := graphfake.New(t.Cleanup)
			graph.RetryAfter = "7"
			graph.Intercept = func(graphfake.Request, int) (graphfake.Answer, bool) {
				return graphfake.Answer{Status: test.status, Body: graphfake.GraphError(test.code)}, true
			}
			client := newTestClient(t, graph, entraid.Config{})
			_, err := sdkgo.RunQuery(newTestDexContext("retry"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: "ada@contoso.com"})
			failure := requireRetry(t, err)
			require.Equal(t, test.kind, failure.Kind)
			require.Contains(t, failure.Message, test.code)
			var retryAfter *dex.RetryAfterError
			require.True(t, errors.As(err, &retryAfter))
			require.Equal(t, 7*time.Second, retryAfter.After)
		})
	}
}

func TestFailuresNeverRepeatProviderMessagesOrTokens(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(graphfake.Request, int) (graphfake.Answer, bool) {
		return graphfake.Answer{Status: http.StatusForbidden, Body: map[string]any{"error": map[string]any{
			"code": "Authorization_RequestDenied", "message": graphfake.MessageSentinel + " " + graphfake.AccessToken,
		}}}, true
	}
	client := newTestClient(t, graph, entraid.Config{})

	result, err := sdkgo.RunQuery(newTestDexContext("safe-failure"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: "ada@contoso.com"})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchProviderRejected, result.Branch)
	require.Equal(t, "provider rejected the request with HTTP 403 (Authorization_RequestDenied)", result.Failure.Message)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
	require.NotContains(t, string(encoded), graphfake.AccessToken)
}

func TestOversizedResponsesRedirectsAndMissingCredentials(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	small := newTestClient(t, graph, entraid.Config{MaxResponseBytes: 64})
	oversized, err := sdkgo.RunQuery(newTestDexContext("oversized"), small.GetUser(), testConnection, entraid.GetUserInput{UserKey: userID})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchInvalidResponse, oversized.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, oversized.Failure.Kind)

	noCredentials, err := entraid.New(entraid.Config{}, sdkgo.StaticCredentialProvider[entraid.Credentials]{}, entraid.WithLocalProviderURL(graph.URL))
	require.NoError(t, err)
	defect, err := sdkgo.RunQuery(newTestDexContext("no-credentials"), noCredentials.GetUser(), testConnection, entraid.GetUserInput{UserKey: userID})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchDefect, defect.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, defect.Failure.Kind)

	graph.Intercept = func(graphfake.Request, int) (graphfake.Answer, bool) {
		return graphfake.Answer{Status: http.StatusFound}, true
	}
	redirected, err := sdkgo.RunQuery(newTestDexContext("redirect"), newTestClient(t, graph, entraid.Config{}).GetUser(), testConnection, entraid.GetUserInput{UserKey: userID})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchProviderRejected, redirected.Branch)
	require.Len(t, graph.Requests(), 2, "the redirect is never followed")
}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (entraid.Credentials, error) {
	return entraid.Credentials{AuthMethodID: entraid.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[entraid.Credentials],
) (entraid.Credentials, error) {
	provider.forcedRefreshes++
	return entraid.Credentials{AuthMethodID: entraid.MicrosoftOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(graphfake.AccessToken)}, nil
}
