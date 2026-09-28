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
	"github.com/superdurable/dex-connectors-library/connectors/mistral"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "SENTINEL-mistral-integration-key"

// TestSummarizeTextExampleRoutesMistralOutcomesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRoutesMistralOutcomesWithRealDex(t *testing.T) {
	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: "mistral-large-2512", AlternateModel: "mistral-small-2603", IsStreaming: true,
		RequestIDHeader: "mistral-correlation-id", ErrorTokenPointers: []string{"/type", "/code"},
	})
	provider := llmtest.NewFakeProvider(t, dialect.CredentialHeader, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "mistral", Name: ConnectionName}
	client, err := mistral.New(mistral.Config{}, sdkgo.StaticCredentialProvider[mistral.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	}, mistral.WithBaseURLForTest(provider.BaseURL()))
	require.NoError(t, err)
	connection, err := mistral.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, SummaryModelConfiguration{})
	dexClient := startSummaryWorker(t, flow)
	usage := llm.Usage{InputTokens: 30, OutputTokens: 12, TotalTokens: 42}

	t.Run("generated summary completes the Flow and streams its text", func(t *testing.T) {
		provider.EnqueueReplies(dialect.GeneratedReply(llmtest.GeneratedReply{Text: "The connector shipped.", ServedModel: "mistral-large-2512", Usage: usage}))
		flowID := fmt.Sprintf("mistral-summary-generated-%d", time.Now().UnixNano())
		outcome := runSummaryFlow(t, dexClient, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: mistral.GenerateTextBranchGenerated, Summary: "The connector shipped.", ServedModel: "mistral-large-2512",
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
		}, 10*time.Second, 100*time.Millisecond, "the streamed summary must reach the mistral-summary-text Stream")
		var display map[string]any
		require.NoError(t, dexClient.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "The connector shipped.")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("model_length finish completes as truncated with the partial text", func(t *testing.T) {
		provider.EnqueueReplies(llmtest.FakeReply{
			Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: `data: {"id":"cmpl-mistral-truncated","object":"chat.completion.chunk","model":"mistral-large-2512",` +
				`"choices":[{"index":0,"delta":{"role":"assistant","content":"The connector"},"finish_reason":"model_length"}],` +
				`"usage":{"prompt_tokens":30,"completion_tokens":12,"total_tokens":42}}` + "\n\ndata: [DONE]\n\n",
		})
		outcome := runSummaryFlow(t, dexClient, flow, fmt.Sprintf("mistral-summary-truncated-%d", time.Now().UnixNano()))
		require.Equal(t, mistral.GenerateTextBranchTruncated, outcome.Branch)
		require.Equal(t, llm.FinishReasonLength, outcome.FinishReason)
		require.Equal(t, "The connector", outcome.Summary)
	})

	t.Run("blocked summary completes without text", func(t *testing.T) {
		provider.EnqueueReplies(dialect.BlockedReply(llmtest.GeneratedReply{ServedModel: "mistral-large-2512", Usage: usage}))
		outcome := runSummaryFlow(t, dexClient, flow, fmt.Sprintf("mistral-summary-blocked-%d", time.Now().UnixNano()))
		require.Equal(t, mistral.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, llm.FinishReasonContentPolicy, outcome.FinishReason)
		require.Empty(t, outcome.Summary)
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

// runSummaryFlow starts the Flow and waits, across server long-poll caps, for its completion output.
func runSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string) SummaryOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The Mistral connector shipped today."}, dex.StartFlowOptions{})
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
