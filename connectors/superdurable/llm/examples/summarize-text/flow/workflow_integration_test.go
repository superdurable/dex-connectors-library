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
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/providerdialecttest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationOpenAIKey    = "sk-proj-SENTINEL-llm-integration-openai"
	integrationAnthropicKey = "sk-ant-SENTINEL-llm-integration-claude"
	integrationGeminiKey    = "AIzaSENTINELLlmIntegrationGemini"
)

// summaryProviders holds one fake provider per route, each expecting only its own key.
type summaryProviders struct {
	openAI, claude, gemini *llmtest.FakeProvider
}

// TestSummarizeTextExampleRoutesEachProviderWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRoutesEachProviderWithRealDex(t *testing.T) {
	providers := summaryProviders{
		openAI: llmtest.NewFakeProvider(t, providerdialecttest.OpenAICredentialHeader, integrationOpenAIKey),
		claude: llmtest.NewFakeProvider(t, providerdialecttest.ClaudeCredentialHeader, integrationAnthropicKey),
		gemini: llmtest.NewFakeProvider(t, providerdialecttest.GeminiCredentialHeader, integrationGeminiKey),
	}
	usage := llm.Usage{InputTokens: 30, OutputTokens: 12, TotalTokens: 42}

	t.Run("the connection model generates with OpenAI and streams the summary", func(t *testing.T) {
		providers.openAI.EnqueueReplies(providerdialecttest.NewOpenAIResponsesDialect().GeneratedReply(llmtest.GeneratedReply{
			Text: "The connector shipped.", ServedModel: "gpt-6-sol-2026-06-01", ResponseID: "resp_llm_example", Usage: usage,
		}))
		flow, client := startSummaryWorker(t, providers, SummaryModelConfiguration{})
		flowID := fmt.Sprintf("llm-summary-generated-%d", time.Now().UnixNano())
		outcome := runSummaryFlowToCompletion(t, client, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: llmrouter.GenerateTextBranchGenerated, Summary: "The connector shipped.", Model: "openai/gpt-6-sol",
			ServedModel: "gpt-6-sol-2026-06-01", FinishReason: llm.FinishReasonStop, Usage: usage,
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
		require.Contains(t, string(encodedDisplay), "openai/gpt-6-sol")
		requireNoIntegrationKey(t, string(encodedDisplay))
	})

	t.Run("a Claude pick completes as blocked without text", func(t *testing.T) {
		providers.claude.EnqueueReplies(providerdialecttest.ClaudeStreamReply(llmtest.GeneratedReply{
			ServedModel: "claude-haiku-4-5", ResponseID: "msg_llm_example", Usage: usage,
		}, "refusal"))
		flow, client := startSummaryWorker(t, providers, SummaryModelConfiguration{Model: "anthropic/claude-haiku-4-5"})
		outcome := runSummaryFlowToCompletion(t, client, flow, fmt.Sprintf("llm-summary-blocked-%d", time.Now().UnixNano()))
		require.Equal(t, llmrouter.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, llm.FinishReasonRefusal, outcome.FinishReason)
		require.Equal(t, "anthropic/claude-haiku-4-5", outcome.Model)
		require.Empty(t, outcome.Summary)
	})

	t.Run("a Gemini pick generates with the picked model", func(t *testing.T) {
		providers.gemini.EnqueueReplies(providerdialecttest.GeminiCandidateReply(llmtest.GeneratedReply{
			Text: "Gemini summarized it.", ServedModel: "gemini-3.8-flash", ResponseID: "gemini-llm-example", Usage: usage,
		}, "STOP"))
		flow, client := startSummaryWorker(t, providers, SummaryModelConfiguration{Model: "gemini/gemini-3.8-flash"})
		outcome := runSummaryFlowToCompletion(t, client, flow, fmt.Sprintf("llm-summary-gemini-%d", time.Now().UnixNano()))
		require.Equal(t, llmrouter.GenerateTextBranchGenerated, outcome.Branch)
		require.Equal(t, "Gemini summarized it.", outcome.Summary)
		require.Equal(t, "gemini/gemini-3.8-flash", outcome.Model)
		requests := providers.gemini.Requests()
		require.Len(t, requests, 1)
		require.Contains(t, requests[0].Path, "/models/gemini-3.8-flash:generateContent")
	})

	t.Run("a rejected key records the failure and fails the Flow", func(t *testing.T) {
		providers.openAI.EnqueueReplies(providerdialecttest.NewOpenAIResponsesDialect().ErrorReply(http.StatusUnauthorized, "Incorrect API key."))
		flow, client := startSummaryWorker(t, providers, SummaryModelConfiguration{Model: "openai/gpt-6-luna"})
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
		require.JSONEq(t, `{"branch":"providerRejected","provider":"openai","kind":"AUTHENTICATION","message":"`+result.ErrorMessage[len("summary generation selected providerRejected: "):]+`"}`,
			string(encodedFailure), "the failure is persisted before the Flow fails")
	})

	require.Len(t, providers.openAI.Requests(), 2)
	require.Len(t, providers.claude.Requests(), 1)
	require.Len(t, providers.gemini.Requests(), 1)
	for _, provider := range []*llmtest.FakeProvider{providers.openAI, providers.claude, providers.gemini} {
		for _, request := range provider.Requests() {
			require.True(t, request.HasCredentialInSlot, "each provider receives only its own key in its own header")
			require.False(t, request.HasCredentialOutsideSlot)
		}
	}
}

func requireNoIntegrationKey(t *testing.T, value string) {
	t.Helper()
	for _, apiKey := range []string{integrationOpenAIKey, integrationAnthropicKey, integrationGeminiKey} {
		require.False(t, strings.Contains(value, apiKey), "an API key reached Flow state")
	}
}

// startSummaryWorker runs the example Flow's Worker on a free local port and returns a Client targeting it.
func startSummaryWorker(t *testing.T, providers summaryProviders, summaryModel SummaryModelConfiguration) (*Flow, *dex.Client) {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "llm", Name: ConnectionName}
	llmClient, err := llmrouter.New(llmrouter.Config{Model: "openai"}, sdkgo.StaticCredentialProvider[llmrouter.Credentials]{
		reference: {
			OpenAIAPIKey: sdkgo.NewSecretString(integrationOpenAIKey), AnthropicAPIKey: sdkgo.NewSecretString(integrationAnthropicKey),
			GeminiAPIKey: sdkgo.NewSecretString(integrationGeminiKey),
		},
	}, llmrouter.WithProviderBaseURLForTest(llmrouter.ProviderOpenAI, providers.openAI.BaseURL()),
		llmrouter.WithProviderBaseURLForTest(llmrouter.ProviderAnthropic, providers.claude.BaseURL()),
		llmrouter.WithProviderBaseURLForTest(llmrouter.ProviderGemini, providers.gemini.BaseURL()))
	require.NoError(t, err)
	connection, err := llmrouter.NewConnection(llmClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, summaryModel)
	serverAddress := cmp.Or(os.Getenv(llmtest.DexFlowServiceAddressEnvironmentVariable), llmtest.DefaultDexFlowServiceAddress)
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
