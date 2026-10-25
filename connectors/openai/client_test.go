// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var openAIConnection = sdkgo.ConnectionRef{Provider: "openai", Name: "default"}

func TestCreateStructuredResponseCapturesUsageAndReceipt(t *testing.T) {
	var requestBody map[string]any
	var clientRequestID string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Values("Idempotency-Key"), "net/http replays a POST that carries Idempotency-Key")
		clientRequestID = request.Header.Get("X-Client-Request-Id")
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
	result, err := sdkgo.RunMutation(
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
	require.Equal(t, string(result.Receipt.CallID), clientRequestID, "the Call ID is sent as X-Client-Request-Id")
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
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("flow-1", "retrieve-response-1"), client.RetrieveResponse(), openAIConnection,
		openai.RetrieveRequest{ResponseID: "resp_known"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.RetrieveResponseBranchFound, result.Branch)
	require.Equal(t, "resp_known", result.Value.ID)
	require.Equal(t, "resp_known", result.Receipt.ProviderObjectID)
}

func TestRetrieveResponseClassifiesTerminalOutcomes(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "not found", status: http.StatusNotFound, body: `{}`, wantBranch: openai.RetrieveResponseBranchNotFound, wantKind: sdkgo.FailureNotFound},
		{name: "provider rejected", status: http.StatusBadRequest, body: `{}`, wantBranch: openai.RetrieveResponseBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection},
		{name: "invalid response", status: http.StatusOK, body: `{"id":`, wantBranch: openai.RetrieveResponseBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			client := newClient(t, server.URL)
			result, err := sdkgo.RunQuery(
				testsupport.NewDexContext("flow-1", "retrieve-response-"+test.name), client.RetrieveResponse(), openAIConnection,
				openai.RetrieveRequest{ResponseID: "resp_known"},
			)
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
		})
	}
}

func TestRetrieveResponseMissingConnectionUsesDefectBranch(t *testing.T) {
	client, err := openai.New(openai.Config{}, sdkgo.StaticCredentialProvider[openai.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(
		testsupport.NewDexContext("flow-1", "missing-connection"), client.RetrieveResponse(), openAIConnection,
		openai.RetrieveRequest{ResponseID: "resp_123"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.RetrieveResponseBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}

func TestMalformedSuccessResponseLeavesMutationUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`{"id":`))
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("flow-1", "malformed-response-1"), client.CreateResponse(), openAIConnection,
		openai.CreateRequest{Model: "gpt-test", Input: "profile"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.NotEmpty(t, result.Receipt.CallID)
}

func TestCreateResponseRetriesProvenUnsentRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("request must not reach the provider")
	}))
	endpoint := server.URL
	server.Close()
	client, err := openai.New(openai.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		openAIConnection: {APIKey: sdkgo.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "unsent"), client.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "request was not sent")
}

func TestCreateResponseRetriesRateLimitAndUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(status)
			}))
			defer server.Close()
			client := newClient(t, server.URL)
			_, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", fmt.Sprint(status)), client.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
			require.Error(t, err)
			require.Contains(t, err.Error(), fmt.Sprint(status))
		})
	}
}

func TestCreateResponseKeepsUnconfirmedServerFailureUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "server-failure"), client.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
}

func TestCreateResponseKeepsDispatchedLostResponseUncertain(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestCount++
		require.Equal(t, http.MethodPost, request.Method)
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	ctx := testsupport.NewDexContext("flow-1", "lost-response")
	result, err := sdkgo.RunMutation(ctx, client.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	ctx.AttemptNo = 2
	result, err = sdkgo.RunMutation(ctx, client.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchUncertain, result.Branch)
	require.Equal(t, 1, requestCount)
}

func TestConfirmedRejectionUsesProviderRejectedBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-Request-Id", "req_rejected")
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("flow-1", "rejected-response-1"), client.CreateResponse(), openAIConnection,
		openai.CreateRequest{Model: "gpt-test", Input: "profile"},
	)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "req_rejected", result.Receipt.ProviderRequestID)
}

// TestCreateResponseSendsTheConnectionModelWhenTheRequestNamesNone keeps the connection's model field meaningful.
func TestCreateResponseSendsTheConnectionModelWhenTheRequestNamesNone(t *testing.T) {
	var sentModels []any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		sentModels = append(sentModels, body["model"])
		_, _ = response.Write([]byte(`{"id":"resp_model","model":"gpt-6-luna","status":"completed","output":[],"usage":{}}`))
	}))
	defer server.Close()
	client, err := openai.New(openai.Config{Endpoint: server.URL, Model: " gpt-6-luna "}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		openAIConnection: {APIKey: sdkgo.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	for index, model := range []string{"", " ", "gpt-6-sol"} {
		result, err := sdkgo.RunMutation(
			testsupport.NewDexContext("flow-1", fmt.Sprintf("connection-model-%d", index)), client.CreateResponse(), openAIConnection,
			openai.CreateRequest{Model: model, Input: "profile"},
		)
		require.NoError(t, err)
		require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch, "failure: %+v", result.Failure)
	}
	require.Equal(t, []any{"gpt-6-luna", "gpt-6-luna", "gpt-6-sol"}, sentModels, "a request's model overrides the connection's model")

	defaultClient := newClient(t, server.URL)
	_, err = sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "connection-model-default"), defaultClient.CreateResponse(),
		openAIConnection, openai.CreateRequest{Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.DefaultConfig().Model, sentModels[len(sentModels)-1], "a blank connection model uses the manifest default")

	_, err = openai.New(openai.Config{Model: "gpt 6"}, sdkgo.StaticCredentialProvider[openai.Credentials]{})
	require.ErrorContains(t, err, "OpenAI model must be")
	invalid, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "connection-model-invalid"), client.CreateResponse(),
		openAIConnection, openai.CreateRequest{Model: "gpt 6", Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchDefect, invalid.Branch)
	require.Len(t, sentModels, 4, "an invalid model selects defect without a request")
}

func newClient(t *testing.T, endpoint string) *openai.Client {
	t.Helper()
	client, err := openai.New(openai.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		openAIConnection: {APIKey: sdkgo.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	return client
}
