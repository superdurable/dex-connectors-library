// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
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
	result, err := connector.RunMutation(
		testsupport.NewDexContext("flow-1", "create-response-1"), client.CreateResponse(), openAIConnection,
		openai.CreateRequest{
			Model: "gpt-test", Input: "profile",
			StructuredOutput: &openai.StructuredOutput{Name: "evaluation", Strict: true, Schema: map[string]any{"type": "object"}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, "resp_123", result.Value.ID)
	require.Equal(t, 3, result.Value.Usage.CachedInputTokens)
	require.Equal(t, 2, result.Value.Usage.ReasoningOutputTokens)
	require.Equal(t, "req_123", result.Receipt.ProviderRequestID)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)
	require.Equal(t, "900", result.Receipt.Metadata["x-ratelimit-remaining-tokens"])
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
	result, err := connector.RunQuery(
		testsupport.NewDexContext("flow-1", "retrieve-response-1"), client.RetrieveResponse(), openAIConnection,
		openai.RetrieveRequest{ResponseID: "resp_known"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.RetrieveResponseBranchFound, result.Branch)
	require.Equal(t, "resp_known", result.Value.ID)
	require.Equal(t, "resp_known", result.Receipt.ProviderObjectID)
}

func TestMalformedSuccessResponseLeavesMutationUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`{"id":`))
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := connector.RunMutation(
		testsupport.NewDexContext("flow-1", "malformed-response-1"), client.CreateResponse(), openAIConnection,
		openai.CreateRequest{Model: "gpt-test", Input: "profile"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Equal(t, connector.FailureProtocol, result.Failure.Kind)
	require.NotEmpty(t, result.Receipt.CallID)
}

func TestConfirmedRejectionIsFailedResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-Request-Id", "req_rejected")
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := connector.RunMutation(
		testsupport.NewDexContext("flow-1", "rejected-response-1"), client.CreateResponse(), openAIConnection,
		openai.CreateRequest{Model: "gpt-test", Input: "profile"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchFailed, result.Branch)
	require.Equal(t, connector.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "req_rejected", result.Receipt.ProviderRequestID)
}

func newClient(t *testing.T, endpoint string) *openai.Client {
	t.Helper()
	client, err := openai.New(openai.Config{Endpoint: endpoint}, connector.StaticCredentialProvider[openai.Credentials]{
		openAIConnection: {APIKey: connector.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	return client
}
