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
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "sk-proj-SENTINEL-integration-key"

// TestSummarizeTextExampleRoutesOpenAIOutcomesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleRoutesOpenAIOutcomesWithRealDex(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "openai", Name: ConnectionName}
	client, err := openai.New(openai.Config{Endpoint: provider.BaseURL()}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	})
	require.NoError(t, err)
	connection, err := openai.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection, SummaryModelConfiguration{})
	dexClient := startSummaryWorker(t, flow)
	usage := llm.Usage{InputTokens: 30, OutputTokens: 12, ReasoningTokens: 4, TotalTokens: 42}

	t.Run("generated summary completes the Flow and streams its text", func(t *testing.T) {
		provider.EnqueueReplies(responsesStreamReply([]string{"The connector ", "shipped."}, "completed", nil, usage))
		flowID := fmt.Sprintf("openai-summary-generated-%d", time.Now().UnixNano())
		outcome := runSummaryFlow(t, dexClient, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: openai.GenerateTextBranchGenerated, Summary: "The connector shipped.", ServedModel: "gpt-6-sol",
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
		}, 10*time.Second, 100*time.Millisecond, "the streamed summary must reach the openai-summary-text Stream")
		var display map[string]any
		require.NoError(t, dexClient.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "The connector shipped.")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("blocked summary completes without text", func(t *testing.T) {
		provider.EnqueueReplies(responsesStreamReply(nil, "incomplete", map[string]any{"reason": "content_filter"}, usage))
		outcome := runSummaryFlow(t, dexClient, flow, fmt.Sprintf("openai-summary-blocked-%d", time.Now().UnixNano()))
		require.Equal(t, openai.GenerateTextBranchBlocked, outcome.Branch)
		require.Equal(t, llm.FinishReasonContentPolicy, outcome.FinishReason)
		require.Empty(t, outcome.Summary)
	})
	requests := provider.Requests()
	require.Len(t, requests, 2)
	var body struct {
		Model string `json:"model"`
		Store *bool  `json:"store"`
	}
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, "gpt-6-sol", body.Model, "the Step without a pick uses the connection's default model")
	require.False(t, *body.Store, "generateText never stores the Response")
}

// responsesStreamReply renders a Responses event stream whose deltas and terminal Response hold the same text.
func responsesStreamReply(deltas []string, status string, incompleteDetails map[string]any, usage llm.Usage) llmtest.FakeReply {
	text := ""
	stream := responsesEvent("response.created", map[string]any{"response": map[string]any{
		"id": "resp_summary", "object": "response", "status": "in_progress", "model": "gpt-6-sol", "output": []any{},
	}})
	for _, delta := range deltas {
		text += delta
		stream += responsesEvent("response.output_text.delta", map[string]any{"item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": delta})
	}
	output := []any{}
	if text != "" {
		output = append(output, map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "status": status,
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}})
	}
	terminalType := "response.completed"
	if status == "incomplete" {
		terminalType = "response.incomplete"
	}
	stream += responsesEvent(terminalType, map[string]any{"response": map[string]any{
		"id": "resp_summary", "object": "response", "status": status, "incomplete_details": incompleteDetails,
		"model": "gpt-6-sol", "output": output, "store": false,
		"usage": map[string]any{
			"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens, "total_tokens": usage.TotalTokens,
			"output_tokens_details": map[string]any{"reasoning_tokens": usage.ReasoningTokens},
		},
	}})
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: stream}
}

func responsesEvent(eventType string, fields map[string]any) string {
	fields["type"] = eventType
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(fmt.Sprintf("encode Responses event: %v", err))
	}
	return "event: " + eventType + "\ndata: " + string(encoded) + "\n\n"
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
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The OpenAI connector shipped today."}, dex.StartFlowOptions{})
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
