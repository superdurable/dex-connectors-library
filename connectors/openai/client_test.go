// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

var openAIConnection = connector.ConnectionRef{Provider: "openai", Name: "default"}

func TestCreateStructuredResponseCapturesUsageAndReceipt(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
		require.NotEmpty(t, request.Header.Get("Idempotency-Key"))
		require.NoError(t, json.NewDecoder(request.Body).Decode(&requestBody))
		response.Header().Set("X-Request-Id", "req_123")
		response.Header().Set("X-RateLimit-Remaining-Tokens", "900")
		_, _ = response.Write([]byte(`{
          "id":"resp_123","model":"gpt-test","status":"completed",
          "output":[{"content":[{"type":"output_text","text":"{\"match\":true}"}]}],
          "usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":3},"output_tokens":7,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":18}
        }`))
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := client.CreateResponse(context.Background(), openai.CreateRequest{
		Connection: openAIConnection, CallID: connector.NewCallID(), Model: "gpt-test", Input: "profile",
		StructuredOutput: &openai.StructuredOutput{Name: "evaluation", Strict: true, Schema: map[string]any{"type": "object"}},
	})
	require.NoError(t, err)
	require.Equal(t, "resp_123", result.Value.ID)
	require.Equal(t, 3, result.Value.Usage.CachedInputTokens)
	require.Equal(t, 2, result.Value.Usage.ReasoningOutputTokens)
	require.Equal(t, "req_123", result.Receipt.ProviderRequestID)
	require.Equal(t, "900", result.Meta["x-ratelimit-remaining-tokens"])
	text, ok := requestBody["text"].(map[string]any)
	require.True(t, ok)
	require.NotNil(t, text["format"])
}

func TestRetrieveResponseSupportsReceiptRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/responses/resp_known", request.URL.Path)
		_, _ = response.Write([]byte(`{"id":"resp_known","model":"gpt-test","status":"completed","output":[],"usage":{}}`))
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := client.RetrieveResponse(context.Background(), openAIConnection, "resp_known")
	require.NoError(t, err)
	require.Equal(t, "resp_known", result.Value.ID)
}

func TestMalformedSuccessResponseLeavesMutationUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`{"id":`))
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	callID := connector.NewCallID()
	_, err := client.CreateResponse(context.Background(), openai.CreateRequest{
		Connection: openAIConnection, CallID: callID, Model: "gpt-test", Input: "profile",
	})
	require.True(t, connector.IsUnknownMutation(err))
	var typed *connector.Error
	require.ErrorAs(t, err, &typed)
	receipt, ok := typed.Receipt()
	require.True(t, ok)
	require.Equal(t, callID, receipt.CallID)
}

func TestConfirmedRejectionHasFailedReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-Request-Id", "req_rejected")
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	callID := connector.NewCallID()
	_, err := client.CreateResponse(context.Background(), openai.CreateRequest{
		Connection: openAIConnection, CallID: callID, Model: "gpt-test", Input: "profile",
	})
	require.True(t, connector.IsKind(err, connector.ErrorAuthentication))
	var typed *connector.Error
	require.ErrorAs(t, err, &typed)
	receipt, ok := typed.Receipt()
	require.True(t, ok)
	require.Equal(t, connector.ActionFailed, receipt.Outcome)
	require.Equal(t, "req_rejected", receipt.ProviderRequestID)
}

func newClient(t *testing.T, endpoint string) *openai.Client {
	t.Helper()
	client, err := openai.New(openai.Config{Endpoint: endpoint}, connector.StaticCredentialProvider{
		openAIConnection: connector.NewCredential(map[string]string{"api_key": "test-key"}),
	})
	require.NoError(t, err)
	return client
}
