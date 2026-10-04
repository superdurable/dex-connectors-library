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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "sk-proj-SENTINEL-integration-key"

// TestSummarizeTextExampleStoresOpenAIResponsesWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestSummarizeTextExampleStoresOpenAIResponsesWithRealDex(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, integrationAPIKey)

	t.Run("a completed Response completes the Flow with the connection's model and streams its text", func(t *testing.T) {
		provider.EnqueueReplies(responsesStreamReply([]string{"The connector ", "shipped."}, "completed", "gpt-6-sol"))
		flow, client := startSummaryWorker(t, provider, SummaryModelConfiguration{})
		flowID := fmt.Sprintf("openai-summary-completed-%d", time.Now().UnixNano())
		outcome := runSummaryFlow(t, client, flow, flowID)
		require.Equal(t, SummaryOutcome{
			Branch: openai.CreateResponseBranchCompleted, Summary: "The connector shipped.", ResponseID: "resp_summary",
			Model: "gpt-6-sol", Status: "completed",
			Usage: openai.Usage{InputTokens: 30, OutputTokens: 12, ReasoningOutputTokens: 4, TotalTokens: 42},
		}, outcome)
		require.Eventually(t, func() bool {
			var page dex.StreamMessagesPage[string]
			err := client.ListStreamMessages(context.Background(), flowID, summaryTextStream, 100, "", &page)
			text := ""
			for index := len(page.Messages) - 1; index >= 0; index-- {
				text += page.Messages[index].Value
			}
			return err == nil && text == "The connector shipped."
		}, 10*time.Second, 100*time.Millisecond, "the streamed summary must reach the openai-summary-text Stream")
		var display map[string]any
		require.NoError(t, client.InvokeRPC(context.Background(), flowID, flow.GetDexDisplay, nil, &display))
		encodedDisplay, err := json.Marshal(display)
		require.NoError(t, err)
		require.Contains(t, string(encodedDisplay), "The connector shipped.")
		require.NotContains(t, string(encodedDisplay), integrationAPIKey)
	})

	t.Run("an incomplete Response for the Step's pick completes the Flow as failed", func(t *testing.T) {
		provider.EnqueueReplies(responsesStreamReply([]string{"Partial"}, "incomplete", "gpt-6-luna"))
		flow, client := startSummaryWorker(t, provider, SummaryModelConfiguration{Model: "gpt-6-luna"})
		outcome := runSummaryFlow(t, client, flow, fmt.Sprintf("openai-summary-failed-%d", time.Now().UnixNano()))
		require.Equal(t, openai.CreateResponseBranchFailed, outcome.Branch)
		require.Equal(t, "incomplete", outcome.Status)
		require.Equal(t, "resp_summary", outcome.ResponseID, "the stored Response stays readable with retrieveResponse")
	})

	requests := provider.Requests()
	require.Len(t, requests, 2)
	for index, expectedModel := range []string{"gpt-6-sol", "gpt-6-luna"} {
		var body struct {
			Model  string `json:"model"`
			Input  string `json:"input"`
			Stream bool   `json:"stream"`
		}
		require.NoError(t, json.Unmarshal(requests[index].Body, &body))
		require.Equal(t, expectedModel, body.Model, "an empty pick sends the connection's model")
		require.Equal(t, "The OpenAI connector shipped today.", body.Input)
		require.True(t, body.Stream, "a text Stream makes createResponse stream")
		require.NotEmpty(t, requests[index].Header.Get("Idempotency-Key"), "createResponse derives an idempotency key from the Call ID")
		require.Equal(t, "/responses", requests[index].Path)
		require.True(t, requests[index].HasCredentialInSlot)
		require.False(t, requests[index].HasCredentialOutsideSlot)
	}
}

// responsesStreamReply renders a Responses event stream whose deltas and terminal Response hold the same text.
func responsesStreamReply(deltas []string, status string, model string) textgentest.FakeReply {
	text := ""
	stream := responsesEvent("response.created", map[string]any{"response": map[string]any{
		"id": "resp_summary", "object": "response", "status": "in_progress", "model": model, "output": []any{},
	}})
	for _, delta := range deltas {
		text += delta
		stream += responsesEvent("response.output_text.delta", map[string]any{"item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": delta})
	}
	terminalType := "response.completed"
	if status == "incomplete" {
		terminalType = "response.incomplete"
	}
	stream += responsesEvent(terminalType, map[string]any{"response": map[string]any{
		"id": "resp_summary", "object": "response", "status": status, "model": model,
		"output": []any{map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "status": status,
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}},
		"usage": map[string]any{
			"input_tokens": 30, "output_tokens": 12, "total_tokens": 42, "output_tokens_details": map[string]any{"reasoning_tokens": 4},
		},
	}})
	return textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: stream}
}

func responsesEvent(eventType string, fields map[string]any) string {
	fields["type"] = eventType
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(fmt.Sprintf("encode Responses event: %v", err))
	}
	return "event: " + eventType + "\ndata: " + string(encoded) + "\n\n"
}

// startSummaryWorker opens the connection from project storage the way the Worker does, points its endpoint
// at provider, runs the example Flow's Worker on a free local port, and returns a Client targeting it.
func startSummaryWorker(t *testing.T, provider *textgentest.FakeProvider, summaryModel SummaryModelConfiguration) (*Flow, *dex.Client) {
	t.Helper()
	project := testsupport.NewLoadedProject(t, openai.ConnectorID, []testsupport.ProjectConnection{{
		Name: ConnectionName, Configuration: map[string]any{"endpoint": provider.BaseURL()},
		Credentials: map[string]any{"api_key": integrationAPIKey},
	}}, nil)
	connection, err := openai.NewProjectConnection(project, ConnectionName)
	require.NoError(t, err)
	flow := NewFlow(connection, summaryModel)
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
