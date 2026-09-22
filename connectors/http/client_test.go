// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package httpconnector_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	mockprovider "github.com/superdurable/dex-connectors-library/test/mock-provider"
)

var mockConnection = connector.ConnectionRef{Provider: "mock", Name: "default"}

func newClient(t *testing.T, baseURL string) *httpconnector.Client {
	t.Helper()
	client, err := httpconnector.New(httpconnector.Config{
		BaseURL:           baseURL,
		CredentialHeaders: map[string]string{"api_key": "X-Mock-Api-Key"},
	}, connector.StaticCredentialProvider{
		mockConnection: connector.NewCredential(map[string]string{"api_key": "test-key"}),
	})
	require.NoError(t, err)
	return client
}

func TestQueryAndIdempotentAction(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())

	query, err := client.Query(context.Background(), http.MethodGet, httpconnector.Request{
		Connection: mockConnection, Path: "/profiles/customer-1",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, query.Value.StatusCode)
	require.Empty(t, query.Value.Header.Get("Set-Cookie"))
	require.Equal(t, "mock-request-1", query.Value.Header.Get("X-Request-Id"))

	callID := connector.NewCallID()
	for range 2 {
		result, actionErr := client.Action(context.Background(), http.MethodPost, callID, httpconnector.Request{
			Connection: mockConnection,
			Path:       "/credits",
			Body:       mockprovider.Action{CustomerID: "customer-1", Credits: 100},
		})
		require.NoError(t, actionErr)
		require.Equal(t, connector.ActionSucceeded, result.Receipt.Outcome)
		var action mockprovider.Action
		require.NoError(t, json.Unmarshal(result.Value.Body, &action))
		require.Equal(t, string(callID), action.CallID)
	}
}

func TestRateLimitIsTypedAndRetryable(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	_, err := client.Query(context.Background(), http.MethodGet, httpconnector.Request{
		Connection: mockConnection, Path: "/rate-limit",
	})
	require.True(t, connector.IsKind(err, connector.ErrorRateLimit))
	var typed *connector.Error
	require.ErrorAs(t, err, &typed)
	require.Equal(t, 3*time.Second, typed.RetryAfter)
}

func TestConfirmedActionRejectionHasFailedReceipt(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	callID := connector.NewCallID()
	_, err := client.Action(context.Background(), http.MethodPost, callID, httpconnector.Request{
		Connection: mockConnection, Path: "/missing", Body: map[string]any{"credits": 1},
	})
	require.True(t, connector.IsKind(err, connector.ErrorNotFound))
	var typed *connector.Error
	require.ErrorAs(t, err, &typed)
	receipt, ok := typed.Receipt()
	require.True(t, ok)
	require.Equal(t, connector.ActionFailed, receipt.Outcome)
	require.Equal(t, "mock-request-1", receipt.ProviderRequestID)
}

func TestActionTransportFailureIsUnknown(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	callID := connector.NewCallID()
	_, err := client.Action(context.Background(), http.MethodPost, callID, httpconnector.Request{
		Connection: mockConnection,
		Path:       "/credits-unknown",
		Body:       mockprovider.Action{CustomerID: "customer-unknown", Credits: 1},
	})
	require.True(t, connector.IsUnknownMutation(err))
	var typed *connector.Error
	require.ErrorAs(t, err, &typed)
	receipt, ok := typed.Receipt()
	require.True(t, ok)
	require.Equal(t, connector.ActionUnknown, receipt.Outcome)

	recovered, recoverErr := client.Query(context.Background(), http.MethodGet, httpconnector.Request{
		Connection: mockConnection, Path: "/actions/" + string(callID),
	})
	require.NoError(t, recoverErr)
	var action mockprovider.Action
	require.NoError(t, json.Unmarshal(recovered.Value.Body, &action))
	require.Equal(t, "succeeded", action.Status)
}

func TestRejectsSecretHeaderInFlowInput(t *testing.T) {
	provider := mockprovider.Start()
	defer provider.Close()
	client := newClient(t, provider.URL())
	_, err := client.Query(context.Background(), http.MethodGet, httpconnector.Request{
		Connection: mockConnection,
		Path:       "/profiles/customer-1",
		Headers:    map[string]string{"Authorization": "Bearer leaked"},
	})
	require.ErrorContains(t, err, "CredentialProvider")
	require.NotContains(t, err.Error(), "leaked")
}
