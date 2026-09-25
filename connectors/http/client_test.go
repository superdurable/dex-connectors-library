// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package httpconnector_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	mockprovider "github.com/superdurable/dex-connectors-library/connectors/http/internal/mockprovider"
	"github.com/superdurable/dex-connectors-library/connectors/http/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var mockConnection = sdkgo.ConnectionRef{Provider: "mock", Name: "default"}

func newClient(t *testing.T, baseURL string) *httpconnector.Client {
	t.Helper()
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: baseURL, CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, sdkgo.StaticCredentialProvider[httpconnector.Credentials]{
		mockConnection: {APIKey: sdkgo.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	return client
}

func TestQueryAndIdempotentMutation(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())

	query, err := sdkgo.RunQuery(
		testsupport.NewDexContext("flow-1", "read-profile-1"), client.Query(), mockConnection,
		httpconnector.Request{Method: http.MethodGet, Path: "/profiles/customer-1"},
	)
	require.NoError(t, err)
	require.Equal(t, httpconnector.QueryBranchSucceeded, query.Branch)
	require.Equal(t, http.StatusOK, query.Value.StatusCode)
	require.Empty(t, query.Value.Header.Get("Set-Cookie"))
	require.Equal(t, "mock-request-1", query.Value.Header.Get("X-Request-Id"))

	ctx := testsupport.NewDexContext("flow-1", "grant-credit-1")
	var firstCallID sdkgo.CallID
	for range 2 {
		result, mutationErr := sdkgo.RunMutation(ctx, client.Mutation(), mockConnection, httpconnector.Request{
			Method: http.MethodPost, Path: "/credits",
			Body: mockprovider.Mutation{CustomerID: "customer-1", Credits: 100},
		})
		require.NoError(t, mutationErr)
		require.Equal(t, httpconnector.MutationBranchSucceeded, result.Branch)
		if firstCallID == "" {
			firstCallID = result.Receipt.CallID
		}
		require.Equal(t, firstCallID, result.Receipt.CallID)
		require.Equal(t, sdkgo.IdempotencyKey(firstCallID), result.Receipt.IdempotencyKey)
		var mutation mockprovider.Mutation
		require.NoError(t, json.Unmarshal(result.Value.Body, &mutation))
		require.Equal(t, string(firstCallID), mutation.CallID)
	}
	require.Equal(t, 1, provider.MutationCount(firstCallID))
}

func TestQueryRateLimitIsTheOnlyGoErrorPath(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	_, err := sdkgo.RunQuery(
		testsupport.NewDexContext("flow-1", "rate-limit-1"), client.Query(), mockConnection,
		httpconnector.Request{Method: http.MethodGet, Path: "/rate-limit"},
	)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 3*time.Second, retryAfter.After)
}

func TestConfirmedMutationRejectionIsFailedResult(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("flow-1", "missing-mutation-1"), client.Mutation(), mockConnection,
		httpconnector.Request{Method: http.MethodPost, Path: "/missing", Body: map[string]any{"credits": 1}},
	)
	require.NoError(t, err)
	require.Equal(t, httpconnector.MutationBranchRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, "mock-request-1", result.Receipt.ProviderRequestID)
}

func TestQueryStatusClassificationIsExplicit(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		wantKind  sdkgo.FailureKind
		wantRetry bool
	}{
		{name: "authentication", status: http.StatusUnauthorized, wantKind: sdkgo.FailureAuthentication},
		{name: "not found", status: http.StatusNotFound, wantKind: sdkgo.FailureNotFound},
		{name: "availability", status: http.StatusServiceUnavailable, wantKind: sdkgo.FailureAvailability, wantRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				response.WriteHeader(test.status)
			}))
			defer server.Close()
			client := newClient(t, server.URL)
			result, err := sdkgo.RunQuery(
				testsupport.NewDexContext("flow-1", "status-query-1"), client.Query(), mockConnection,
				httpconnector.Request{Method: http.MethodGet, Path: "/resource"},
			)
			if test.wantRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.wantKind, retry.Failure.Kind)
			} else {
				require.NoError(t, err)
				require.Equal(t, httpconnector.QueryBranchFailed, result.Branch)
				require.Equal(t, test.wantKind, result.Failure.Kind)
			}
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestCommittedMutationWithLostResponseIsUnknownAndRecoverable(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("flow-1", "unknown-mutation-1"), client.Mutation(), mockConnection,
		httpconnector.Request{
			Method: http.MethodPost, Path: "/credits-unknown",
			Body: mockprovider.Mutation{CustomerID: "customer-unknown", Credits: 1},
		},
	)
	require.NoError(t, err)
	require.Equal(t, httpconnector.MutationBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)

	recovered, recoverErr := sdkgo.RunQuery(
		testsupport.NewDexContext("flow-1", "recover-mutation-1"), client.Query(), mockConnection,
		httpconnector.Request{Method: http.MethodGet, Path: "/mutations/" + string(result.Receipt.CallID)},
	)
	require.NoError(t, recoverErr)
	require.Equal(t, httpconnector.QueryBranchSucceeded, recovered.Branch)
	var mutation mockprovider.Mutation
	require.NoError(t, json.Unmarshal(recovered.Value.Body, &mutation))
	require.Equal(t, "succeeded", mutation.Status)
	require.Equal(t, 1, provider.MutationCount(result.Receipt.CallID))
}

func TestRejectsSecretHeaderInFlowInput(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("flow-1", "secret-header-1"), client.Query(), mockConnection,
		httpconnector.Request{
			Method: http.MethodGet, Path: "/profiles/customer-1",
			Headers: map[string]string{"Authorization": "Bearer leaked"},
		},
	)
	require.NoError(t, err)
	require.Equal(t, httpconnector.QueryBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "CredentialProvider")
	require.NotContains(t, result.Failure.Message, "leaked")
}

func TestCredentialFailureBeforeDispatchIsNotUnknown(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, err := httpconnector.New(httpconnector.Config{BaseURL: server.URL}, failingCredentialProvider{})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("flow-1", "credential-failure-1"), client.Mutation(), mockConnection,
		httpconnector.Request{Method: http.MethodPost, Path: "/credits", Body: map[string]int{"credits": 1}},
	)
	require.NoError(t, err)
	require.Equal(t, httpconnector.MutationBranchRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "credential store unavailable")
	require.Zero(t, requests.Load())
}

func TestProviderSpecificIdempotencyKeyIsSentAndReceipted(t *testing.T) {
	var key string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		key = request.Header.Get("Idempotency-Key")
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: server.URL,
	}, sdkgo.StaticCredentialProvider[httpconnector.Credentials]{mockConnection: {}}, httpconnector.WithIdempotencyKeyFunc(
		func(callID sdkgo.CallID, _ httpconnector.Request) sdkgo.IdempotencyKey {
			return sdkgo.IdempotencyKey("provider_" + string(callID))
		},
	))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("flow-1", "provider-key-1"), client.Mutation(), mockConnection,
		httpconnector.Request{Method: http.MethodPost, Path: "/credits"},
	)
	require.NoError(t, err)
	require.Equal(t, httpconnector.MutationBranchSucceeded, result.Branch)
	require.Equal(t, key, string(result.Receipt.IdempotencyKey))
	require.Contains(t, key, "provider_")
}

type failingCredentialProvider struct{}

func (failingCredentialProvider) Resolve(sdkgo.Call) (httpconnector.Credentials, error) {
	return httpconnector.Credentials{}, errors.New("credential store unavailable")
}
