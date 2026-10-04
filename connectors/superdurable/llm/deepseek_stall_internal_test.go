// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

const deepSeekInternalTestAPIKey = "sk-deepseek-internal-test-key-0123456789"

var deepSeekInternalTestConnection = sdkgo.ConnectionRef{Provider: ConnectorID, Name: "deepseek-internal-test"}

// shortDeepSeekStallTimeout stands in for deepSeekStallTimeout, so the tests observe the stall rule in milliseconds.
const shortDeepSeekStallTimeout = 300 * time.Millisecond

func TestDeepSeekStallRuleSitsBetweenKeepAlivesAndTheRequestTimeout(t *testing.T) {
	require.Equal(t, deepSeekStallTimeout, deepSeekChatProfile.StallTimeout)
	deepSeek := providerAPIs[ProviderDeepseek]
	require.Equal(t, queuedStreamingRequestTimeout, deepSeek.requestTimeout)
	require.Greater(t, deepSeek.requestTimeout, deepSeekChatProfile.StallTimeout+10*time.Minute,
		"a request that queues for DeepSeek's 10 minutes still has time to stall and retry")
	require.Less(t, deepSeek.requestTimeout, GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout,
		"a stalled exchange returns Retry before Dex abandons the attempt")
}

// TestDeepSeekKeepAliveCommentsHoldAQueuedStreamOpen replays a queue longer than the stall timeout that stays alive through keep-alive comments.
func TestDeepSeekKeepAliveCommentsHoldAQueuedStreamOpen(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, deepSeekInternalTestAPIKey)
	keepAlive := textgentest.FakeStreamChunk{Delay: shortDeepSeekStallTimeout / 3, Data: ": keep-alive\n\n"}
	provider.EnqueueReplies(textgentest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}},
		StreamChunks: []textgentest.FakeStreamChunk{
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
		newShortStallDeepSeekQuery(t, provider.BaseURL()), deepSeekInternalTestConnection, deepSeekInternalUserRequest())
	require.NoError(t, err)
	require.Equal(t, GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "Queued answer.", result.Value.Text)
	require.Equal(t, textgen.Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8}, result.Value.Usage)
}

// TestDeepSeekSilentStreamReturnsRetry proves the stall rule, not the request timeout, ends a stream without keep-alives.
func TestDeepSeekSilentStreamReturnsRetry(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, deepSeekInternalTestAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body:   ": keep-alive\n\n",
		StreamChunks: []textgentest.FakeStreamChunk{
			{Delay: shortDeepSeekStallTimeout * 10, Data: ": keep-alive\n\n"},
		},
	})
	startedAt := time.Now()
	_, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", "deepseek-silent-stream"),
		newShortStallDeepSeekQuery(t, provider.BaseURL()), deepSeekInternalTestConnection, deepSeekInternalUserRequest())
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, sdkgo.FailureAvailability, retryError.Failure.Kind)
	require.Less(t, time.Since(startedAt), shortDeepSeekStallTimeout*5, "the stall rule, not the request timeout, ended the attempt")
}

// newShortStallDeepSeekQuery builds the DeepSeek Query from deepSeekChatProfile with only the stall timeout shortened.
func newShortStallDeepSeekQuery(t *testing.T, baseURL string) *textgen.TextGenerationQuery {
	t.Helper()
	profile := deepSeekChatProfile
	profile.StallTimeout = shortDeepSeekStallTimeout
	wireFormat, err := openaichat.NewWireFormat(&profile)
	require.NoError(t, err)
	deepSeek := providerAPIs[ProviderDeepseek]
	query, err := textgen.NewTextGenerationQuery(&textgen.TextGenerationQueryConfig{
		Definition: GenerateTextDefinition, WireFormat: wireFormat, BaseURL: baseURL, ConnectionModel: deepSeek.defaultModel,
		RequestTimeout: deepSeek.requestTimeout,
		ResolveCredential: func(sdkgo.Call) (sdkgo.SecretString, error) {
			return sdkgo.NewSecretString(deepSeekInternalTestAPIKey), nil
		},
		MaxResponseBytes: deepSeek.defaultMaxResponseBytes, MaxStreamEventBytes: maxStreamEventBytes,
	})
	require.NoError(t, err)
	return query
}

func deepSeekInternalUserRequest() textgen.TextGenerationRequest {
	return textgen.TextGenerationRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Hello"}}}
}
