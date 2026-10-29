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
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
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

	t.Run("an incomplete Response for the Step's pick fails the Flow", func(t *testing.T) {
		provider.EnqueueReplies(responsesStreamReply([]string{"Partial"}, "incomplete", "gpt-6-luna"))
		flow, client := startSummaryWorker(t, provider, SummaryModelConfiguration{Model: "gpt-6-luna"})
		flowID := fmt.Sprintf("openai-summary-failed-%d", time.Now().UnixNano())
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The OpenAI connector shipped today."}, dex.StartFlowOptions{})
		require.NoError(t, err)
		result := waitForSummaryResult(t, ctx, client, flowID)
		require.Equal(t, dex.FlowFailed, result.Status)
		require.Contains(t, result.ErrorMessage, "openai createResponse (connection openai-api) selected branch failed with no target")
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
		require.NotEmpty(t, requests[index].Header.Get("X-Client-Request-Id"), "createResponse sends the Call ID for OpenAI support lookups")
		require.Empty(t, requests[index].Header.Values("Idempotency-Key"), "net/http replays a POST that carries Idempotency-Key")
		require.Equal(t, "/responses", requests[index].Path)
		require.True(t, requests[index].HasCredentialInSlot)
		require.False(t, requests[index].HasCredentialOutsideSlot)
	}
}

func TestSummarizeTextExampleConfirmsInterruptedStreamWithRealDex(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, integrationAPIKey)
	provider.EnqueueReplies(
		textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: responsesEvent("response.created", map[string]any{"response": map[string]any{"id": "resp_recovered", "status": "in_progress"}})},
		textgentest.FakeReply{Body: `{"id":"resp_recovered","model":"gpt-6-sol","status":"completed","output":[{"content":[{"type":"output_text","text":"Recovered summary."}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`},
	)
	flow, client := startSummaryWorker(t, provider, SummaryModelConfiguration{})
	outcome := runSummaryFlow(t, client, flow, fmt.Sprintf("openai-summary-recovered-%d", time.Now().UnixNano()))
	require.Equal(t, openai.CreateResponseBranchCompleted, outcome.Branch)
	require.Equal(t, "resp_recovered", outcome.ResponseID)
	require.Equal(t, "Recovered summary.", outcome.Summary)
	requests := provider.Requests()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, http.MethodGet, requests[1].Method)
	require.Equal(t, "/responses/resp_recovered", requests[1].Path)
}

const uncertainSummaryReason = "openai createResponse (connection openai-api) selected branch uncertain with no target"

// TestSummarizeTextExampleFailsClosedAfterWorkerLossBeforeAResponseIDWithRealDex
// loses the Worker after OpenAI received the create request but before any Response ID.
func TestSummarizeTextExampleFailsClosedAfterWorkerLossBeforeAResponseIDWithRealDex(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, integrationAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{
		Delay: 10 * time.Minute, Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: responsesEvent("response.created", map[string]any{"response": map[string]any{"id": "resp_unseen", "status": "in_progress"}}),
	})
	client, flowID, ctx := startSummaryFlowAndLoseWorker(t, provider, func(*dex.Client, string) bool { return len(provider.Requests()) == 1 })
	result := waitForSummaryResult(t, ctx, client, flowID)
	require.Equal(t, dex.FlowFailed, result.Status, "the unwired uncertain branch fails the Flow")
	require.Contains(t, result.ErrorMessage, uncertainSummaryReason)
	require.Equal(t, []string{http.MethodPost}, providerMethods(provider), "a Response may exist, so the next attempt must not create another")
}

// TestSummarizeTextExampleNeverResendsAfterWorkerLossWithRealDex loses the Worker
// after it queued the Response ID checkpoint, whose persistence no Dex Client API shows.
func TestSummarizeTextExampleNeverResendsAfterWorkerLossWithRealDex(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, integrationAPIKey)
	created := responsesEvent("response.created", map[string]any{"response": map[string]any{"id": "resp_resumed", "status": "in_progress"}})
	delta := responsesEvent("response.output_text.delta", map[string]any{"item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": "Partial "})
	provider.EnqueueReplies(
		textgentest.FakeReply{
			Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: created + delta,
			StreamChunks: []textgentest.FakeStreamChunk{{Delay: time.Minute, Data: responsesEvent("response.completed", map[string]any{"response": map[string]any{"id": "resp_resumed", "status": "completed"}})}},
		},
		textgentest.FakeReply{Body: `{"id":"resp_resumed","model":"gpt-6-sol","status":"completed","output":[{"content":[{"type":"output_text","text":"Resumed summary."}]}]}`},
	)
	client, flowID, ctx := startSummaryFlowAndLoseWorker(t, provider, func(client *dex.Client, flowID string) bool {
		// The Worker sends the Response ID checkpoint before the text on one ordered stream.
		var page dex.StreamMessagesPage[string]
		err := client.ListStreamMessages(context.Background(), flowID, summaryTextStream, 100, "", &page)
		return err == nil && len(page.Messages) > 0
	})
	result := waitForSummaryResult(t, ctx, client, flowID)
	methods := providerMethods(provider)
	require.NotEmpty(t, methods)
	require.Equal(t, http.MethodPost, methods[0])
	require.NotContains(t, methods[1:], http.MethodPost, "the next attempt must never create another billed Response")
	// Reading the Response back and failing closed are both correct; only a second POST is not.
	if result.Status == dex.FlowFailed {
		t.Log("Dex had not persisted the Response ID checkpoint; the resumed attempt failed closed")
		require.Contains(t, result.ErrorMessage, uncertainSummaryReason)
		require.Equal(t, []string{http.MethodPost}, methods)
		return
	}
	t.Log("the resumed attempt read the checkpointed Response back")
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome SummaryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, openai.CreateResponseBranchCompleted, outcome.Branch)
	require.Equal(t, "resp_resumed", outcome.ResponseID)
	require.Equal(t, "Resumed summary.", outcome.Summary)
	require.Equal(t, []string{http.MethodPost, http.MethodGet}, methods)
	require.Equal(t, "/responses/resp_resumed", provider.Requests()[1].Path)
}

// startSummaryFlowAndLoseWorker starts the Flow, stops its Worker once isReadyToLose holds, and starts a new Worker.
func startSummaryFlowAndLoseWorker(t *testing.T, provider *textgentest.FakeProvider, isReadyToLose func(*dex.Client, string) bool) (*dex.Client, string, context.Context) {
	t.Helper()
	flow := NewFlow(newSummaryConnection(t, provider), SummaryModelConfiguration{})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	stopFirstWorker := runSummaryWorker(t, registry)
	client := newSummaryClient(t, registry)
	flowID := fmt.Sprintf("openai-summary-worker-loss-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	_, err = client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The OpenAI connector shipped today."}, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return isReadyToLose(client, flowID) }, 30*time.Second, 50*time.Millisecond)
	stoppedContext, stopNow := context.WithCancel(context.Background())
	stopNow()
	require.ErrorIs(t, stopFirstWorker(stoppedContext), context.Canceled)
	runSummaryWorker(t, registry)
	return client, flowID, ctx
}

func providerMethods(provider *textgentest.FakeProvider) []string {
	requests := provider.Requests()
	methods := make([]string, 0, len(requests))
	for _, request := range requests {
		methods = append(methods, request.Method)
	}
	return methods
}

// TestSummarizeTextExampleResendsOnlyAfterProvenNoResponseWithRealDex checks that a 429 and a cancelled Response each let the next attempt send again.
func TestSummarizeTextExampleResendsOnlyAfterProvenNoResponseWithRealDex(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, textgen.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}, integrationAPIKey)

	t.Run("a 429 retries with a new request", func(t *testing.T) {
		provider.EnqueueReplies(
			textgentest.FakeReply{StatusCode: http.StatusTooManyRequests, Body: `{"error":{"type":"rate_limit"}}`},
			responsesStreamReply([]string{"After the limit."}, "completed", "gpt-6-sol"),
		)
		flow, client := startSummaryWorker(t, provider, SummaryModelConfiguration{})
		outcome := runSummaryFlow(t, client, flow, fmt.Sprintf("openai-summary-rate-limited-%d", time.Now().UnixNano()))
		require.Equal(t, openai.CreateResponseBranchCompleted, outcome.Branch)
		require.Equal(t, "After the limit.", outcome.Summary)
	})

	t.Run("a cancelled Response retries with a new request", func(t *testing.T) {
		provider.EnqueueReplies(
			textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: responsesEvent("response.created", map[string]any{"response": map[string]any{"id": "resp_cancelled", "status": "in_progress"}})},
			textgentest.FakeReply{Body: `{"id":"resp_cancelled","status":"cancelled"}`},
			responsesStreamReply([]string{"After the cancellation."}, "completed", "gpt-6-sol"),
		)
		flow, client := startSummaryWorker(t, provider, SummaryModelConfiguration{})
		outcome := runSummaryFlow(t, client, flow, fmt.Sprintf("openai-summary-cancelled-%d", time.Now().UnixNano()))
		require.Equal(t, openai.CreateResponseBranchCompleted, outcome.Branch)
		require.Equal(t, "After the cancellation.", outcome.Summary)
	})

	requests := provider.Requests()
	require.Equal(t, []string{http.MethodPost, http.MethodPost, http.MethodPost, http.MethodGet, http.MethodPost}, providerMethods(provider))
	require.Equal(t, requests[0].Header.Get("X-Client-Request-Id"), requests[1].Header.Get("X-Client-Request-Id"), "retries of one Step execution share the Call ID")
	require.Equal(t, requests[2].Header.Get("X-Client-Request-Id"), requests[4].Header.Get("X-Client-Request-Id"))
	require.NotEqual(t, requests[0].Header.Get("X-Client-Request-Id"), requests[2].Header.Get("X-Client-Request-Id"))
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
// at provider, runs the example Flow on a pull Worker, and returns a Client.
func startSummaryWorker(t *testing.T, provider *textgentest.FakeProvider, summaryModel SummaryModelConfiguration) (*Flow, *dex.Client) {
	t.Helper()
	return startSummaryWorkerWithConnection(t, newSummaryConnection(t, provider), summaryModel)
}

func startSummaryWorkerWithConnection(t *testing.T, connection openai.Connection, summaryModel SummaryModelConfiguration) (*Flow, *dex.Client) {
	t.Helper()
	flow := NewFlow(connection, summaryModel)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	runSummaryWorker(t, registry)
	return flow, newSummaryClient(t, registry)
}

func newSummaryConnection(t *testing.T, provider *textgentest.FakeProvider) openai.Connection {
	t.Helper()
	project := testsupport.NewLoadedProject(t, openai.ConnectorID, []testsupport.ProjectConnection{{
		Name: ConnectionName, Configuration: map[string]any{"endpoint": provider.BaseURL()},
		Credentials: map[string]any{"api_key": integrationAPIKey},
	}}, nil)
	connection, err := openai.NewProjectConnection(project, ConnectionName)
	require.NoError(t, err)
	return connection
}

// runSummaryWorker starts a Worker and returns its stop function; a cancelled context stops it at once.
func runSummaryWorker(t *testing.T, registry *dex.Registry) func(context.Context) error {
	t.Helper()
	cache := newBlobCache(t)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: dexServerAddress()})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	require.NoError(t, worker.WaitPullRegistered(t.Context()))
	var isStopped atomic.Bool
	stop := func(ctx context.Context) error {
		isStopped.Store(true)
		return errors.Join(worker.Stop(ctx), <-workerResult, cache.Close())
	}
	t.Cleanup(func() {
		if isStopped.Load() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, stop(ctx))
	})
	return stop
}

func newSummaryClient(t *testing.T, registry *dex.Registry) *dex.Client {
	t.Helper()
	cache := newBlobCache(t)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: dexServerAddress(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	return client
}

func newBlobCache(t *testing.T) *blobcache.Cache {
	t.Helper()
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	return cache
}

func dexServerAddress() string {
	return cmp.Or(os.Getenv(textgentest.DexFlowServiceAddressEnvironmentVariable), textgentest.DefaultDexFlowServiceAddress)
}

// runSummaryFlow starts the Flow and waits, across server long-poll caps, for its completion output.
func runSummaryFlow(t *testing.T, client *dex.Client, flow *Flow, flowID string) SummaryOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, err := client.StartFlow(ctx, flow, flowID, SummaryRequest{Text: "The OpenAI connector shipped today."}, dex.StartFlowOptions{})
	require.NoError(t, err)
	return waitForSummaryOutcome(t, ctx, client, flowID)
}

// waitForSummaryOutcome waits, across server long-poll caps, for the Flow's completion output.
func waitForSummaryOutcome(t *testing.T, ctx context.Context, client *dex.Client, flowID string) SummaryOutcome {
	t.Helper()
	result := waitForSummaryResult(t, ctx, client, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, "Flow %s: %s", flowID, result.ErrorMessage)
	var outcome SummaryOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	return outcome
}

// waitForSummaryResult waits, across server long-poll caps, for the Flow to close.
func waitForSummaryResult(t *testing.T, ctx context.Context, client *dex.Client, flowID string) dex.FlowResult {
	t.Helper()
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
