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
	"github.com/superdurable/dex-connectors-library/internal/testsupport"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	mockprovider "github.com/superdurable/dex-connectors-library/test/mock-provider"
)

var mockConnection = connector.ConnectionRef{Provider: "mock", Name: "default"}

func newClient(t *testing.T, baseURL string) *httpconnector.Client {
	t.Helper()
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL: baseURL, CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider{
		mockConnection: connector.NewCredential(map[string]string{"api_key": "test-key"}),
	})
	require.NoError(t, err)
	return client
}

func TestQueryAndIdempotentMutation(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())

	query, err := connector.RunQuery(
		testsupport.NewDexContext("flow-1", "read-profile-1"), client.Query(), mockConnection,
		httpconnector.Request{Method: http.MethodGet, Path: "/profiles/customer-1"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, query.Value.StatusCode)
	require.Empty(t, query.Value.Header.Get("Set-Cookie"))
	require.Equal(t, "mock-request-1", query.Value.Header.Get("X-Request-Id"))

	ctx := testsupport.NewDexContext("flow-1", "grant-credit-1")
	var firstCallID connector.CallID
	for range 2 {
		result, mutationErr := connector.RunMutation(ctx, client.Mutation(), mockConnection, httpconnector.Request{
			Method: http.MethodPost, Path: "/credits",
			Body: mockprovider.Mutation{CustomerID: "customer-1", Credits: 100},
		})
		require.NoError(t, mutationErr)
		require.Equal(t, connector.MutationSucceeded, result.Outcome)
		if firstCallID == "" {
			firstCallID = result.Receipt.CallID
		}
		require.Equal(t, firstCallID, result.Receipt.CallID)
		var mutation mockprovider.Mutation
		require.NoError(t, json.Unmarshal(result.Value.Body, &mutation))
		require.Equal(t, string(firstCallID), mutation.CallID)
	}
	require.Equal(t, 1, provider.MutationCount(firstCallID))
}

func TestRateLimitIsTypedAndConvertsToDexRetry(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	_, err := connector.RunQuery(
		testsupport.NewDexContext("flow-1", "rate-limit-1"), client.Query(), mockConnection,
		httpconnector.Request{Method: http.MethodGet, Path: "/rate-limit"},
	)
	require.True(t, connector.IsKind(err, connector.ErrorRateLimit))
	retry, ok := connector.DexRetry(err, time.Second)
	require.True(t, ok)
	require.Equal(t, 3*time.Second, retry.After)
}

func TestConfirmedMutationRejectionIsFailedResult(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	result, err := connector.RunMutation(
		testsupport.NewDexContext("flow-1", "missing-mutation-1"), client.Mutation(), mockConnection,
		httpconnector.Request{Method: http.MethodPost, Path: "/missing", Body: map[string]any{"credits": 1}},
	)
	require.NoError(t, err)
	require.Equal(t, connector.MutationFailed, result.Outcome)
	require.Equal(t, connector.ErrorNotFound, result.Failure.Kind)
	require.Equal(t, "mock-request-1", result.Receipt.ProviderRequestID)
}

func TestCommittedMutationWithLostResponseIsUnknownAndRecoverable(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	result, err := connector.RunMutation(
		testsupport.NewDexContext("flow-1", "unknown-mutation-1"), client.Mutation(), mockConnection,
		httpconnector.Request{
			Method: http.MethodPost, Path: "/credits-unknown",
			Body: mockprovider.Mutation{CustomerID: "customer-unknown", Credits: 1},
		},
	)
	require.NoError(t, err)
	require.Equal(t, connector.MutationUnknown, result.Outcome)
	require.Equal(t, connector.ErrorUnknownMutation, result.Failure.Kind)

	recovered, recoverErr := connector.RunQuery(
		testsupport.NewDexContext("flow-1", "recover-mutation-1"), client.Query(), mockConnection,
		httpconnector.Request{Method: http.MethodGet, Path: "/mutations/" + string(result.Receipt.CallID)},
	)
	require.NoError(t, recoverErr)
	var mutation mockprovider.Mutation
	require.NoError(t, json.Unmarshal(recovered.Value.Body, &mutation))
	require.Equal(t, "succeeded", mutation.Status)
	require.Equal(t, 1, provider.MutationCount(result.Receipt.CallID))
}

func TestRejectsSecretHeaderInFlowInput(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	_, err := connector.RunQuery(
		testsupport.NewDexContext("flow-1", "secret-header-1"), client.Query(), mockConnection,
		httpconnector.Request{
			Method: http.MethodGet, Path: "/profiles/customer-1",
			Headers: map[string]string{"Authorization": "Bearer leaked"},
		},
	)
	require.ErrorContains(t, err, "CredentialProvider")
	require.NotContains(t, err.Error(), "leaked")
}

func TestCredentialFailureBeforeDispatchIsNotUnknown(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, err := httpconnector.New(httpconnector.Config{BaseURL: server.URL}, failingCredentialProvider{})
	require.NoError(t, err)
	result, err := connector.RunMutation(
		testsupport.NewDexContext("flow-1", "credential-failure-1"), client.Mutation(), mockConnection,
		httpconnector.Request{Method: http.MethodPost, Path: "/credits", Body: map[string]int{"credits": 1}},
	)
	require.ErrorContains(t, err, "credential store unavailable")
	require.NotEqual(t, connector.MutationUnknown, result.Outcome)
	require.Zero(t, requests.Load())
}

type failingCredentialProvider struct{}

func (failingCredentialProvider) Resolve(connector.Call) (connector.Credential, error) {
	return connector.Credential{}, errors.New("credential store unavailable")
}
