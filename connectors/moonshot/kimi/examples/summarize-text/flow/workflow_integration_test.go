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
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "sk-SENTINEL-kimi-integration-key"

// TestSummarizeTextExampleRoutesKimiOutcomesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRoutesKimiOutcomesWithRealDex(t *testing.T) {
	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: "kimi-k2.6", AlternateModel: "kimi-k3", IsStreaming: true,
	})
	provider := llmtest.NewFakeProvider(t, dialect.CredentialHeader, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "moonshot", Name: ConnectionName}
	client, err := kimi.New(kimi.Config{}, sdkgo.StaticCredentialProvider[kimi.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	}, kimi.WithBaseURLForTest(provider.BaseURL()))
	require.NoError(t, err)
	connection, err := kimi.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, SummaryModelConfiguration{})
	dexClient := startSummaryWorker(t, flow)
	usage := llm.Usage{InputTokens: 30, CachedInputTokens: 10, OutputTokens: 12, TotalTokens: 42}

	t.Run("generated summary completes the Flow and streams its text", func(t *testing.T) {
		provider.EnqueueReplies(dialect.GeneratedReply(llmtest.GeneratedReply{Text: "The connector shipped.", ServedModel: "kimi-k2.6", Usage: usage}))
		flowID := fmt.Sprintf("kimi-summary-generated-%d", time.Now().UnixNano())
		outcome := runSummaryFlow(t, dexClient, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: kimi.GenerateTextBranchGenerated, Summary: "The connector shipped.", ServedModel: "kimi-k2.6",
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
		}, 10*time.Second, 100*time.Millisecond, "the streamed summary must reach the kimi-summary-text Stream")
		var display map[string]any
		require.NoError(t, dexClient.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "The connector shipped.")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("content_filter rejection completes as blocked without text", func(t *testing.T) {
		provider.EnqueueReplies(llmtest.FakeReply{
			StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
			Body: `{"error":{"message":"The request was rejected because it was considered high risk","type":"content_filter"}}`,
		})
		outcome := runSummaryFlow(t, dexClient, flow, fmt.Sprintf("kimi-summary-blocked-%d", time.Now().UnixNano()))
		require.Equal(t, kimi.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, llm.FinishReasonContentPolicy, outcome.FinishReason)
		require.Empty(t, outcome.Summary)
	})

	t.Run("exhausted balance fails the Flow without a retry", func(t *testing.T) {
		requestsBefore := len(provider.Requests())
		provider.EnqueueReplies(llmtest.FakeReply{
			StatusCode: http.StatusTooManyRequests, Header: http.Header{"Content-Type": {"application/json"}},
			Body: `{"error":{"message":"Token quota is insufficient","type":"exceeded_current_quota_error"}}`,
		})
		flowID := fmt.Sprintf("kimi-summary-quota-%d", time.Now().UnixNano())
		result := waitForSummaryFlow(t, dexClient, flow, flowID)
		require.Equal(t, dex.FlowFailed, result.Status, "the unwired providerRejected branch fails the Flow")
		require.Len(t, provider.Requests(), requestsBefore+1, "a quota rejection is never retried")
	})
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

// runSummaryFlow starts the Flow and returns its completion output.
func runSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string) SummaryOutcome {
	t.Helper()
	result := waitForSummaryFlow(t, client, flow, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome SummaryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

// waitForSummaryFlow starts the Flow and waits, across server long-poll caps, for it to close.
func waitForSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The Kimi connector shipped today."}, dex.StartFlowOptions{})
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
