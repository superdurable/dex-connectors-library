//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/messagestest"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat/openaichattest"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "sk-SENTINEL-llm-summary-integration"

// Each dialect answers like its provider's API, as the connector's own wire-format tests pin.
var (
	deepSeekIntegrationDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: "deepseek-flash", AlternateModel: "deepseek-v4-pro", IsStreaming: true, RequestIDHeader: "x-ds-trace-id",
	})
	mistralIntegrationDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: "mistral-large-2512", AlternateModel: "mistral-small-2603", IsStreaming: true,
		RequestIDHeader: "mistral-correlation-id", ErrorTokenPointers: []string{"/type", "/code"},
	})
	kimiIntegrationDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
		ConnectionModel: "kimi-k2.6", AlternateModel: "kimi-k3", IsStreaming: true,
	})
	claudeIntegrationDialect = messagestest.NewProviderDialect("claude-sonnet-5", "claude-haiku-4-5")
)

// TestSummarizeTextExampleRunsOnTheConnectionsProviderWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRunsOnTheConnectionsProviderWithRealDex(t *testing.T) {
	usage := textgen.Usage{InputTokens: 30, OutputTokens: 12, TotalTokens: 42}

	t.Run("the provider's default model generates and streams the summary", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, deepSeekIntegrationDialect.CredentialHeader, integrationAPIKey)
		provider.EnqueueReplies(deepSeekIntegrationDialect.GeneratedReply(textgentest.GeneratedReply{
			Text: "The connector shipped.", ServedModel: "deepseek-flash", ResponseID: "chatcmpl-llm-example", Usage: usage,
		}))
		flow, client := startSummaryWorker(t, provider, map[string]any{"provider": "deepseek"}, SummaryModelConfiguration{})
		flowID := fmt.Sprintf("llm-summary-generated-%d", time.Now().UnixNano())
		outcome := runSummaryFlowToCompletion(t, client, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: llm.GenerateTextBranchGenerated, Summary: "The connector shipped.", Provider: "deepseek", Model: "deepseek-flash",
			ServedModel: "deepseek-flash", FinishReason: textgen.FinishReasonStop, Usage: usage,
		}, outcome)
		require.Eventually(t, func() bool {
			var page dex.StreamMessagesPage[string]
			err := client.ListStreamMessages(context.Background(), flowID, summaryTextStream, 100, "", &page)
			text := ""
			for index := len(page.Messages) - 1; index >= 0; index-- {
				text += page.Messages[index].Value
			}
			return err == nil && text == "The connector shipped."
		}, 10*time.Second, 100*time.Millisecond, "the streamed summary must reach the llm-summary-text Stream")
		var display map[string]any
		require.NoError(t, client.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), `"model":"deepseek-flash"`)
		requireNoIntegrationKey(t, string(encodedDisplay))
		requireOnlyKeyInItsSlot(t, provider)
	})

	t.Run("the connection model applies to a Step without a pick", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, mistralIntegrationDialect.CredentialHeader, integrationAPIKey)
		provider.EnqueueReplies(mistralIntegrationDialect.GeneratedReply(textgentest.GeneratedReply{
			Text: "Mistral summarized it.", ServedModel: "mistral-small-2603", ResponseID: "cmpl-llm-connection-model", Usage: usage,
		}))
		flow, client := startSummaryWorker(t, provider, map[string]any{"provider": "mistral", "model": "mistral-small-2603"},
			SummaryModelConfiguration{})
		outcome := runSummaryFlowToCompletion(t, client, flow, fmt.Sprintf("llm-summary-connection-model-%d", time.Now().UnixNano()))
		require.Equal(t, llm.GenerateTextBranchGenerated, outcome.Branch)
		require.Equal(t, "mistral", outcome.Provider)
		require.Equal(t, "mistral-small-2603", outcome.Model)
		requireOnlyKeyInItsSlot(t, provider)
	})

	t.Run("a Claude Step pick completes as blocked without text", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, claudeIntegrationDialect.CredentialHeader, integrationAPIKey)
		provider.EnqueueReplies(messagestest.StreamReply(textgentest.GeneratedReply{
			ServedModel: "claude-haiku-4-5", ResponseID: "msg_llm_example", Usage: usage,
		}, "refusal"))
		flow, client := startSummaryWorker(t, provider, map[string]any{"provider": "anthropic"},
			SummaryModelConfiguration{Model: "claude-haiku-4-5"})
		outcome := runSummaryFlowToCompletion(t, client, flow, fmt.Sprintf("llm-summary-blocked-%d", time.Now().UnixNano()))
		require.Equal(t, llm.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, textgen.FinishReasonRefusal, outcome.FinishReason)
		require.Equal(t, "anthropic", outcome.Provider)
		require.Equal(t, "claude-haiku-4-5", outcome.Model)
		require.Empty(t, outcome.Summary)
		requireOnlyKeyInItsSlot(t, provider)
	})

	t.Run("a rejected key records the failure and fails the Flow", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, kimiIntegrationDialect.CredentialHeader, integrationAPIKey)
		provider.EnqueueReplies(kimiIntegrationDialect.ErrorReply(http.StatusUnauthorized, "Incorrect API key."))
		flow, client := startSummaryWorker(t, provider, map[string]any{"provider": "kimi"}, SummaryModelConfiguration{Model: "kimi-k3"})
		flowID := fmt.Sprintf("llm-summary-rejected-%d", time.Now().UnixNano())
		result := runSummaryFlow(t, client, flow, flowID)
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "summary generation selected providerRejected")
		require.NotContains(t, result.ErrorMessage, "Incorrect API key", "a Failure never carries provider message text")
		requireNoIntegrationKey(t, result.ErrorMessage)
		var display map[string]any
		require.NoError(t, client.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedFailure, err := json.Marshal(display["llm-summary-failure"])
		require.NoError(t, err)
		require.JSONEq(t, `{"branch":"providerRejected","provider":"kimi","kind":"AUTHENTICATION","message":"`+
			result.ErrorMessage[len("summary generation selected providerRejected: "):]+`"}`,
			string(encodedFailure), "the failure is persisted before the Flow fails")
		requireOnlyKeyInItsSlot(t, provider)
	})

	t.Run("a Step pick the provider's model rule rejects records a defect without a request", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, claudeIntegrationDialect.CredentialHeader, integrationAPIKey)
		flow, client := startSummaryWorker(t, provider, map[string]any{"provider": "anthropic"},
			SummaryModelConfiguration{Model: "claude sonnet 5"})
		flowID := fmt.Sprintf("llm-summary-defect-%d", time.Now().UnixNano())
		result := runSummaryFlow(t, client, flow, flowID)
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "summary generation selected defect")
		require.NotContains(t, result.ErrorMessage, "claude sonnet 5", "a Failure never repeats the model value")
		var display map[string]any
		require.NoError(t, client.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedFailure, err := json.Marshal(display["llm-summary-failure"])
		require.NoError(t, err)
		require.Contains(t, string(encodedFailure), `"provider":"anthropic"`)
		require.Contains(t, string(encodedFailure), `"branch":"defect"`)
		require.Empty(t, provider.Requests(), "a defect selected before dispatch sends nothing")
	})
}

func requireNoIntegrationKey(t *testing.T, value string) {
	t.Helper()
	require.False(t, strings.Contains(value, integrationAPIKey), "an API key reached Flow state")
}

func requireOnlyKeyInItsSlot(t *testing.T, provider *textgentest.FakeProvider) {
	t.Helper()
	require.NotEmpty(t, provider.Requests())
	for _, request := range provider.Requests() {
		require.True(t, request.HasCredentialInSlot, "the provider receives the key in its credential header")
		require.False(t, request.HasCredentialOutsideSlot)
	}
}

// startSummaryWorker opens the connection from project storage the way the Worker does, points it at
// provider, runs the example Flow's Worker on a free local port, and returns a Client targeting it.
func startSummaryWorker(
	t *testing.T, provider *textgentest.FakeProvider, configuration map[string]any, summaryModel SummaryModelConfiguration,
) (*Flow, *dex.Client) {
	t.Helper()
	project := testsupport.NewLoadedProject(t, llm.ConnectorID, []testsupport.ProjectConnection{{
		Name: ConnectionName, Configuration: configuration, Credentials: map[string]any{"api_key": integrationAPIKey},
	}}, nil)
	llmConnection, err := llm.NewProjectConnection(project, ConnectionName, llm.WithBaseURLForTest(provider.BaseURL()))
	require.NoError(t, err)
	return startSummaryWorkerWithConnection(t, llmConnection, summaryModel)
}

// startSummaryWorkerWithConnection registers the Flow with one llm Connection on a new Worker.
func startSummaryWorkerWithConnection(t *testing.T, llmConnection llm.Connection, summaryModel SummaryModelConfiguration) (*Flow, *dex.Client) {
	t.Helper()
	flow := NewFlow(llmConnection, summaryModel)
	serverAddress := cmp.Or(os.Getenv(textgentest.DexFlowServiceAddressEnvironmentVariable), textgentest.DefaultDexFlowServiceAddress)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
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
	return flow, client
}

func runSummaryFlowToCompletion(t *testing.T, client *dex.Client, flow *Flow, flowID string) SummaryOutcome {
	t.Helper()
	result := runSummaryFlow(t, client, flow, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome SummaryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

// runSummaryFlow starts the Flow and waits, across server long-poll caps, until it closes.
func runSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string) dex.FlowResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The LLM connector shipped today."}, dex.StartFlowOptions{})
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
