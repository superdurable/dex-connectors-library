//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "AIzaSENTINEL-integration-key"

// TestSummarizeTextExampleRoutesGeminiOutcomesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRoutesGeminiOutcomesWithRealDex(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, llm.CredentialHeader{Name: "x-goog-api-key"}, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	client, err := gemini.New(gemini.Config{Endpoint: provider.BaseURL() + "/v1beta"}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	})
	require.NoError(t, err)
	connection, err := gemini.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, SummaryModelConfiguration{})
	dexClient := startSummaryWorker(t, flow)
	usage := llm.Usage{InputTokens: 30, OutputTokens: 12, ReasoningTokens: 4, TotalTokens: 42}

	t.Run("generated summary completes the Flow and writes its text to the Stream", func(t *testing.T) {
		provider.EnqueueReplies(geminiReply(`{"candidates":[{"content":{"role":"model","parts":[` +
			`{"text":"Reasoning stays private.","thought":true},{"text":"The connector "},{"text":"shipped."}]},"finishReason":"STOP"}],` +
			`"usageMetadata":{"promptTokenCount":30,"candidatesTokenCount":8,"thoughtsTokenCount":4,"totalTokenCount":42},` +
			`"modelVersion":"gemini-3.5-flash-lite","responseId":"resp-summary-1"}`))
		flowID := fmt.Sprintf("gemini-text-summary-generated-%d", time.Now().UnixNano())
		outcome := runSummaryFlow(t, dexClient, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: gemini.GenerateTextBranchGenerated, Summary: "The connector shipped.", ServedModel: "gemini-3.5-flash-lite",
			FinishReason: llm.FinishReasonStop, ProviderFinishReason: "STOP", Usage: usage,
		}, outcome)
		require.Eventually(t, func() bool {
			var page dex.StreamMessagesPage[string]
			err := dexClient.ListStreamMessages(context.Background(), flowID, summaryTextStream, 100, "", &page)
			text := ""
			for index := len(page.Messages) - 1; index >= 0; index-- {
				text += page.Messages[index].Value
			}
			return err == nil && text == "The connector shipped."
		}, 10*time.Second, 100*time.Millisecond, "the summary must reach the gemini-text-summary-text Stream")
		var display map[string]any
		require.NoError(t, dexClient.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "The connector shipped.")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
		require.NotContains(t, string(encodedDisplay), "Reasoning stays private.")
	})

	t.Run("blocked prompt completes without text", func(t *testing.T) {
		provider.EnqueueReplies(geminiReply(`{"promptFeedback":{"blockReason":"SAFETY"},` +
			`"usageMetadata":{"promptTokenCount":30,"totalTokenCount":30},"modelVersion":"gemini-3.5-flash-lite"}`))
		outcome := runSummaryFlow(t, dexClient, flow, fmt.Sprintf("gemini-text-summary-blocked-%d", time.Now().UnixNano()))
		require.Equal(t, gemini.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, llm.FinishReasonContentPolicy, outcome.FinishReason)
		require.Equal(t, "blockReason:SAFETY", outcome.ProviderFinishReason)
		require.Empty(t, outcome.Summary)
	})

	requests := provider.Requests()
	require.Len(t, requests, 2)
	for _, request := range requests {
		require.Equal(t, "/v1beta/models/gemini-3.5-flash-lite:generateContent", request.Path, "the connection's default model applies")
		require.True(t, request.HasCredentialInSlot)
		require.False(t, request.HasCredentialOutsideSlot)
	}
}

func geminiReply(body string) llmtest.FakeReply {
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
}

// startSummaryWorker runs a Worker for flow on a free local port and returns a Client that targets it.
func startSummaryWorker(t *testing.T, flow *Flow) *dex.Client {
	t.Helper()
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: workerAddress, FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	return client
}

// runSummaryFlow starts the Flow and waits, across server long-poll caps, for its completion output.
func runSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string) SummaryOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The Gemini connector shipped today."}, dex.StartFlowOptions{})
	require.NoError(t, err)
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
		var outcome SummaryOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		return outcome
	}
}
