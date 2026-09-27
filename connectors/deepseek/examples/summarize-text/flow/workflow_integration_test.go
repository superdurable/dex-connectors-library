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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "sk-SENTINEL-deepseek-integration-key"

// TestSummarizeTextExampleRoutesDeepSeekOutcomesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRoutesDeepSeekOutcomesWithRealDex(t *testing.T) {
	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: "deepseek-flash", AlternateModel: "deepseek-v4-pro", IsStreaming: true, RequestIDHeader: "x-ds-trace-id",
	})
	provider := llmtest.NewFakeProvider(t, dialect.CredentialHeader, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "deepseek", Name: ConnectionName}
	client, err := deepseek.New(deepseek.Config{}, sdkgo.StaticCredentialProvider[deepseek.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	}, deepseek.WithBaseURLForTest(provider.BaseURL()))
	require.NoError(t, err)
	connection, err := deepseek.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, SummaryModelConfiguration{})
	dexClient := startSummaryWorker(t, flow)
	usage := llm.Usage{InputTokens: 30, CachedInputTokens: 10, OutputTokens: 12, TotalTokens: 42}

	t.Run("generated summary completes the Flow and streams its text", func(t *testing.T) {
		provider.EnqueueReplies(dialect.GeneratedReply(llmtest.GeneratedReply{Text: "The connector shipped.", ServedModel: "deepseek-flash", Usage: usage}))
		flowID := fmt.Sprintf("deepseek-summary-generated-%d", time.Now().UnixNano())
		outcome := decodeSummaryOutcome(t, runSummaryFlow(t, dexClient, flow, flowID, "The DeepSeek connector shipped today."))
		require.Equal(t, SummaryOutcome{
			Branch: deepseek.GenerateTextBranchGenerated, Summary: "The connector shipped.", ServedModel: "deepseek-flash",
			FinishReason: llm.FinishReasonStop, Usage: usage,
		}, outcome)
		require.Eventually(t, func() bool {
			var page dex.StreamMessagesPage[string]
			err := dexClient.ListStreamMessages(context.Background(), flowID, summaryTextStream, 100, "", &page)
			text := ""
			for index := len(page.Messages) - 1; index >= 0; index-- {
				text += page.Messages[index].Value
			}
			return err == nil && text == "The connector shipped."
		}, 10*time.Second, 100*time.Millisecond, "the streamed summary must reach the deepseek-summary-text Stream")
		var display map[string]any
		require.NoError(t, dexClient.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "The connector shipped.")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("blocked summary completes without text", func(t *testing.T) {
		provider.EnqueueReplies(dialect.BlockedReply(llmtest.GeneratedReply{ServedModel: "deepseek-flash", Usage: usage}))
		outcome := decodeSummaryOutcome(t, runSummaryFlow(t, dexClient, flow,
			fmt.Sprintf("deepseek-summary-blocked-%d", time.Now().UnixNano()), "The DeepSeek connector shipped today."))
		require.Equal(t, deepseek.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, llm.FinishReasonContentPolicy, outcome.FinishReason)
		require.Empty(t, outcome.Summary)
	})

	t.Run("insufficient balance fails the Flow through the unwired providerRejected branch without a retry", func(t *testing.T) {
		provider.EnqueueReplies(dialect.QuotaExhaustedReply("Insufficient Balance"))
		result := runSummaryFlow(t, dexClient, flow,
			fmt.Sprintf("deepseek-summary-balance-%d", time.Now().UnixNano()), "The DeepSeek connector shipped today.")
		require.Equal(t, dex.FlowFailed, result.Status)
		require.NotContains(t, result.ErrorMessage, integrationAPIKey)
	})
	require.Len(t, provider.Requests(), 3, "the 402 reply is not retried")

	t.Run("empty text fails before calling DeepSeek", func(t *testing.T) {
		result := runSummaryFlow(t, dexClient, flow, fmt.Sprintf("deepseek-summary-empty-%d", time.Now().UnixNano()), "  ")
		require.Equal(t, dex.FlowFailed, result.Status)
	})
	require.Len(t, provider.Requests(), 3)
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

// runSummaryFlow starts the Flow and waits, across server long-poll caps, for its terminal result.
func runSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string, text string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: text}, dex.StartFlowOptions{})
	require.NoError(t, err)
	for {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var longPollTimeout *dex.LongPollTimeoutError
		if errors.As(err, &longPollTimeout) {
			continue
		}
		require.NoError(t, err)
		return result
	}
}

func decodeSummaryOutcome(t *testing.T, result dex.FlowResult) SummaryOutcome {
	t.Helper()
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow failed: %s", result.ErrorMessage)
	var outcome SummaryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}
