// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

// passthroughMaxResponseBytes keeps the oversized case small.
const passthroughMaxResponseBytes = 64 << 10

// TestRoutedAttemptsEqualDirectProviderConnectorCalls requires identical outcomes and requests, except Call ID and time, for direct and routed calls.
func TestRoutedAttemptsEqualDirectProviderConnectorCalls(t *testing.T) {
	for _, routed := range routedProviders {
		dialect := routed.dialect
		reply := llmtest.GeneratedReply{
			Text: "The passthrough answer.", ServedModel: dialect.AlternateModel, ResponseID: "resp-passthrough",
			Usage: llm.Usage{InputTokens: 11, CachedInputTokens: 3, OutputTokens: 7, ReasoningTokens: 2, TotalTokens: 18},
		}
		rateLimited := dialect.ErrorReply(http.StatusTooManyRequests, "Slow down.")
		rateLimited.Header = rateLimited.Header.Clone()
		rateLimited.Header.Set("Retry-After", "7")
		replies := map[string]llmtest.FakeReply{
			"generated": dialect.GeneratedReply(reply), "truncated": dialect.TruncatedReply(reply), "blocked": dialect.BlockedReply(reply),
			"400": dialect.ErrorReply(http.StatusBadRequest, "Bad request."), "401": dialect.ErrorReply(http.StatusUnauthorized, "Bad key."),
			"403": dialect.ErrorReply(http.StatusForbidden, "Forbidden."), "quota": dialect.QuotaExhaustedReply("Out of credit."),
			"429 with Retry-After": rateLimited, "500": dialect.ErrorReply(http.StatusInternalServerError, "Server error."),
			"malformed 2xx": dialect.MalformedReply(), "dropped connection": {ShouldDropConnection: true},
			"oversized": dialect.GeneratedReply(llmtest.GeneratedReply{Text: strings.Repeat("x", passthroughMaxResponseBytes+1)}),
		}
		if dialect.ReportedErrorReply != nil {
			replies["error inside a 2xx response"] = dialect.ReportedErrorReply("passthrough_reported_error", "Reported.")
		}
		if dialect.InterruptedStreamReply != nil {
			replies["interrupted stream"] = dialect.InterruptedStreamReply(reply)
		}
		for name, providerReply := range replies {
			t.Run(string(routed.provider)+" "+name, func(t *testing.T) {
				direct, directFake := runDirectProviderConnector(t, routed, providerReply)
				viaRouter, routedFake := runThroughRouter(t, routed, providerReply)
				require.Equal(t, direct.result.Branch, viaRouter.result.Branch)
				require.Equal(t, direct.result.Value, viaRouter.result.Value)
				require.Equal(t, direct.result.Failure, viaRouter.result.Failure)
				if viaRouter.err == nil {
					require.Equal(t, routed.connectorID, viaRouter.result.Receipt.Provider, "a branch carries the serving connector's Receipt")
				}
				require.Equal(t, receiptWithoutCallIdentity(direct.result.Receipt), receiptWithoutCallIdentity(viaRouter.result.Receipt))
				requireSameRetry(t, direct.err, viaRouter.err)
				directRequests, routedRequests := directFake.Requests(), routedFake.Requests()
				require.Len(t, routedRequests, len(directRequests))
				for index := range directRequests {
					require.Equal(t, directRequests[index].Method, routedRequests[index].Method)
					require.Equal(t, directRequests[index].Path, routedRequests[index].Path)
					require.Equal(t, directRequests[index].Header, routedRequests[index].Header)
					require.Equal(t, string(directRequests[index].Body), string(routedRequests[index].Body))
					require.Equal(t, directRequests[index].HasCredentialInSlot, routedRequests[index].HasCredentialInSlot)
				}
			})
		}
	}
}

type attemptOutcome struct {
	result llm.TextGenerationResult
	err    error
}

func runDirectProviderConnector(t *testing.T, routed routedProvider, reply llmtest.FakeReply) (attemptOutcome, *llmtest.FakeProvider) {
	t.Helper()
	fake := llmtest.NewFakeProvider(t, routed.dialect.CredentialHeader, routed.apiKey)
	fake.EnqueueReplies(reply)
	apiKey := sdkgo.NewSecretString(routed.apiKey)
	var query sdkgo.Query[llm.TextGenerationRequest, llm.TextGenerationResponse]
	switch routed.provider {
	case llmrouter.ProviderOpenAI:
		client, err := openai.New(openai.Config{Endpoint: fake.BaseURL(), MaxResponseBytes: passthroughMaxResponseBytes},
			sdkgo.StaticCredentialProvider[openai.Credentials]{testConnection: {APIKey: apiKey}})
		require.NoError(t, err)
		query = client.GenerateText()
	case llmrouter.ProviderAnthropic:
		client, err := claude.New(claude.Config{MaxResponseBytes: passthroughMaxResponseBytes},
			sdkgo.StaticCredentialProvider[claude.Credentials]{testConnection: {APIKey: apiKey}}, claude.WithBaseURLForTest(fake.BaseURL()))
		require.NoError(t, err)
		query = client.GenerateText()
	case llmrouter.ProviderGemini:
		client, err := gemini.New(gemini.Config{Endpoint: fake.BaseURL(), MaxResponseBytes: passthroughMaxResponseBytes},
			sdkgo.StaticCredentialProvider[gemini.Credentials]{testConnection: {APIKey: apiKey}})
		require.NoError(t, err)
		query = client.GenerateText()
	}
	result, err := sdkgo.RunQuery(newPassthroughContext(), query, testConnection, userRequest(routed.dialect.AlternateModel, "Hello"))
	return attemptOutcome{result: result, err: err}, fake
}

func runThroughRouter(t *testing.T, routed routedProvider, reply llmtest.FakeReply) (attemptOutcome, *llmtest.FakeProvider) {
	t.Helper()
	fakes := newProviderFakes(t)
	fakes.providers[routed.provider].EnqueueReplies(reply)
	client := newRoutedClient(t, fakes, llmrouter.Config{Model: "openai", MaxResponseBytes: passthroughMaxResponseBytes},
		staticCredentials(allTestAPIKeys()))
	result, err := sdkgo.RunQuery(newPassthroughContext(), client.GenerateText(), testConnection,
		userRequest(string(routed.provider)+"/"+routed.dialect.AlternateModel, "Hello"))
	requireNoAPIKey(t, result, err)
	for _, other := range routedProvidersExcept(routed.provider) {
		require.Empty(t, fakes.providers[other].Requests())
	}
	return attemptOutcome{result: result, err: err}, fakes.providers[routed.provider]
}

func newPassthroughContext() dex.Context {
	return testsupport.NewDexContext("llm-passthrough-flow", fmt.Sprintf("llm-passthrough-step-%d", time.Now().UnixNano()))
}

// receiptWithoutCallIdentity drops the fields that differ because the Call belongs to another connector and time.
func receiptWithoutCallIdentity(receipt sdkgo.Receipt) sdkgo.Receipt {
	receipt.CallID, receipt.ObservedAt = "", time.Time{}
	return receipt
}

func requireSameRetry(t *testing.T, direct error, viaRouter error) {
	t.Helper()
	if direct == nil {
		require.NoError(t, viaRouter)
		return
	}
	var directRetry, routedRetry *sdkgo.RetryError
	require.ErrorAs(t, direct, &directRetry)
	require.ErrorAs(t, viaRouter, &routedRetry)
	require.Equal(t, directRetry.Failure, routedRetry.Failure)
	var directDelay, routedDelay *dex.RetryAfterError
	require.Equal(t, errors.As(direct, &directDelay), errors.As(viaRouter, &routedDelay))
	if directDelay != nil {
		require.Equal(t, directDelay.After, routedDelay.After)
	}
}
