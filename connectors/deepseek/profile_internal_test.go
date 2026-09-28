// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deepseek

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
)

const internalTestAPIKey = "sk-deepseek-internal-test-key-0123456789"

var internalTestConnection = sdkgo.ConnectionRef{Provider: ConnectorID, Name: "deepseek-internal-test"}

// shortStallTimeout stands in for stallTimeout, so the tests observe the stall rule in milliseconds.
const shortStallTimeout = 300 * time.Millisecond

func TestStallRuleSitsBetweenKeepAlivesAndTheRequestTimeout(t *testing.T) {
	require.Equal(t, stallTimeout, chatProfile.StallTimeout)
	require.Greater(t, requestTimeout, chatProfile.StallTimeout+10*time.Minute,
		"a request that queues for DeepSeek's 10 minutes still has time to stall and retry")
}

// TestKeepAliveCommentsHoldAQueuedStreamOpen replays a queue longer than the stall timeout that stays alive through keep-alive comments.
func TestKeepAliveCommentsHoldAQueuedStreamOpen(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, internalTestAPIKey)
	keepAlive := llmtest.FakeStreamChunk{Delay: shortStallTimeout / 3, Data: ": keep-alive\n\n"}
	provider.EnqueueReplies(llmtest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}},
		StreamChunks: []llmtest.FakeStreamChunk{
			keepAlive, keepAlive, keepAlive, keepAlive, keepAlive, keepAlive,
			{Data: `data: {"id":"queued-1","object":"chat.completion.chunk","model":"deepseek-flash",` +
				`"choices":[{"index":0,"delta":{"role":"assistant","content":"Queued answer."},"finish_reason":null}]}` + "\n\n"},
			keepAlive,
			{Data: `data: {"id":"queued-1","object":"chat.completion.chunk","model":"deepseek-flash",` +
				`"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],` +
				`"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}` + "\n\ndata: [DONE]\n\n"},
		},
	})
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", "deepseek-queued-stream"),
		newShortStallQuery(t, provider.BaseURL()), internalTestConnection, internalUserRequest())
	require.NoError(t, err)
	require.Equal(t, GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "Queued answer.", result.Value.Text)
	require.Equal(t, llm.Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8}, result.Value.Usage)
}

// TestSilentStreamReturnsRetry proves the stall rule, not the request timeout, ends a stream without keep-alives.
func TestSilentStreamReturnsRetry(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, internalTestAPIKey)
	provider.EnqueueReplies(llmtest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body:   ": keep-alive\n\n",
		StreamChunks: []llmtest.FakeStreamChunk{
			{Delay: shortStallTimeout * 10, Data: ": keep-alive\n\n"},
		},
	})
	startedAt := time.Now()
	_, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", "deepseek-silent-stream"),
		newShortStallQuery(t, provider.BaseURL()), internalTestConnection, internalUserRequest())
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, sdkgo.FailureAvailability, retryError.Failure.Kind)
	require.Less(t, time.Since(startedAt), shortStallTimeout*5, "the stall rule, not the request timeout, ended the attempt")
}

// newShortStallQuery builds the connector's Query from chatProfile with only the stall timeout shortened.
func newShortStallQuery(t *testing.T, baseURL string) *llm.TextGenerationQuery {
	t.Helper()
	profile := chatProfile
	profile.StallTimeout = shortStallTimeout
	wireFormat, err := openaichat.NewWireFormat(&profile)
	require.NoError(t, err)
	query, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: wireFormat, BaseURL: baseURL, ConnectionModel: DefaultConfig().Model,
		RequestTimeout: requestTimeout,
		ResolveCredential: func(sdkgo.Call) (sdkgo.SecretString, error) {
			return sdkgo.NewSecretString(internalTestAPIKey), nil
		},
		MaxResponseBytes: DefaultConfig().MaxResponseBytes, MaxStreamEventBytes: maxStreamEventBytes,
	})
	require.NoError(t, err)
	return query
}

func internalUserRequest() llm.TextGenerationRequest {
	return llm.TextGenerationRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "Hello"}}}
}
