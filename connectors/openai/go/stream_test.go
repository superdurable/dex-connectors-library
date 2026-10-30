// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var streamTestRef = sdkgo.OperationRef{ConnectorID: "openai", OperationID: "streamFixture"}

var streamTestDefinition = sdkgo.MutationDefinition{
	Operation: streamTestRef,
	Branches: []sdkgo.BranchDefinition{
		{ID: CreateResponseBranchCompleted, Description: "completed"},
		{ID: CreateResponseBranchFailed, Description: "failed"},
		{ID: CreateResponseBranchUncertain, Description: "uncertain"},
		{ID: CreateResponseBranchDefect, Description: "defect"},
	},
}

type streamAttemptOperation struct {
	attempt sdkgo.MutationAttempt[Response]
}

func (streamAttemptOperation) Definition() sdkgo.MutationDefinition {
	return streamTestDefinition
}

func (streamAttemptOperation) IdempotencyKey(id sdkgo.CallID, _ struct{}) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (operation streamAttemptOperation) Invoke(sdkgo.Call, struct{}) sdkgo.MutationAttempt[Response] {
	return operation.attempt
}

func TestOpenAIStreamCompletedUsesFinalResponse(t *testing.T) {
	body := strings.NewReader(sse(
		`{"type":"response.created","sequence_number":1,"response":{"id":"resp_1","status":"in_progress"}}`,
		`{"type":"response.output_text.delta","sequence_number":2,"delta":"hel"}`,
		`{"type":"future.event","sequence_number":3}`,
		`{"type":"response.output_text.delta","sequence_number":4,"delta":"lo"}`,
		`{"type":"response.completed","sequence_number":5,"response":{"id":"resp_1","model":"gpt-test","status":"completed","output":[{"content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
	))
	result := runStreamAttempt(t, body)
	require.Equal(t, CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, "resp_1", result.Value.ID)
	require.Equal(t, "hello", result.Value.OutputText)
	require.Equal(t, 3, result.Value.Usage.TotalTokens)
	require.Equal(t, "resp_1", result.Receipt.ProviderObjectID)
}

func TestOpenAIStreamTerminalFailuresPreserveKnownResponse(t *testing.T) {
	for _, eventType := range []string{"response.failed", "response.incomplete"} {
		t.Run(eventType, func(t *testing.T) {
			result := runStreamAttempt(t, strings.NewReader(sse(
				`{"type":"`+eventType+`","sequence_number":2,"response":{"id":"resp_failed","model":"gpt-test","status":"failed","usage":{"total_tokens":7}}}`,
			)))
			require.Equal(t, CreateResponseBranchFailed, result.Branch)
			require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
			require.Equal(t, "resp_failed", result.Value.ID)
			require.Equal(t, 7, result.Value.Usage.TotalTokens)
			require.Equal(t, "resp_failed", result.Receipt.ProviderObjectID)
		})
	}
}

// An error event after a 2xx stream does not prove that no billed response exists.
func TestOpenAIStreamErrorWithoutResponseIDIsUncertain(t *testing.T) {
	errorEvent := `{"type":"error","sequence_number":2,"error":{"message":"secret provider body"}}`
	for name, events := range map[string][]string{
		"error only":                        {errorEvent},
		"malformed response.created":        {`{"type":"response.created","response":{"id":`, errorEvent},
		"response.created without an ID":    {`{"type":"response.created","response":{"status":"in_progress"}}`, errorEvent},
		"response.created with an empty ID": {`{"type":"response.created","response":{"id":"","status":"in_progress"}}`, errorEvent},
	} {
		t.Run(name, func(t *testing.T) {
			result := runStreamAttempt(t, strings.NewReader(sse(events...)))
			require.Equal(t, CreateResponseBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, "streamed an error before naming a response")
			require.NotContains(t, result.Failure.Message, "secret provider body")
			require.Empty(t, result.Receipt.ProviderObjectID)
		})
	}
}

func TestOpenAIStreamErrorBeforeAResponseIDKeepsReading(t *testing.T) {
	result := runStreamAttempt(t, strings.NewReader(sse(
		`{"type":"response.created","response":{"id":`,
		`{"type":"error"}`,
		`{"type":"response.in_progress","response":{"id":"resp_late","status":"in_progress"}}`,
		`{"type":"response.completed","response":{"id":"resp_late","status":"completed","output":[{"content":[{"type":"output_text","text":"late"}]}]}}`,
	)))
	require.Equal(t, CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, "resp_late", result.Value.ID)
	require.Equal(t, "late", result.Value.OutputText)
}

func TestOpenAIStreamEarlyEOFAndBoundsAreUnknown(t *testing.T) {
	for name, test := range map[string]struct {
		client *Client
		body   string
	}{
		"early eof without response ID": {
			client: &Client{maxResponseBytes: 1024, maxSSEEventBytes: 512},
			body:   sse(`{"type":"response.queued","response":{}}`),
		},
		"event limit": {
			client: &Client{maxResponseBytes: 1024, maxSSEEventBytes: 16},
			body:   sse(`{"type":"response.created","response":{"id":"resp_known"}}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			call := sdkgo.Call{Context: testsupport.NewDexContext("stream-flow", "stream-step")}
			attempt := CreateResponseOperation{client: test.client}.readStream(call, newTestHeartbeat(call), strings.NewReader(test.body), nil, "req_1")
			result, err := sdkgo.RunMutation(
				testsupport.NewDexContext("stream-flow", "stream-step"),
				streamAttemptOperation{attempt: attempt}, sdkgo.ConnectionRef{Provider: "openai", Name: "default"}, struct{}{},
			)
			require.NoError(t, err)
			require.Equal(t, CreateResponseBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
		})
	}
}

func TestOpenAIInterruptedStreamReadsKnownResponse(t *testing.T) {
	for _, test := range []struct {
		name       string
		responses  []string
		streamBody string
		wantBranch sdkgo.BranchID
	}{
		{name: "completed", responses: []string{`{"id":"resp_known","status":"completed","output":[{"content":[{"type":"output_text","text":"confirmed"}]}]}`}, wantBranch: CreateResponseBranchCompleted},
		{name: "still running then completed", responses: []string{`{"id":"resp_known","status":"in_progress"}`, `{"id":"resp_known","status":"completed"}`}, wantBranch: CreateResponseBranchCompleted},
		{name: "failed", responses: []string{`{"id":"resp_known","status":"failed"}`}, wantBranch: CreateResponseBranchFailed},
		{name: "streaming error with known ID", responses: []string{`{"id":"resp_known","status":"completed"}`}, streamBody: sse(`{"type":"response.created","response":{"id":"resp_known","status":"in_progress"}}`, `{"type":"error"}`), wantBranch: CreateResponseBranchCompleted},
	} {
		t.Run(test.name, func(t *testing.T) {
			readCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/responses/resp_known", request.URL.Path)
				_, err := response.Write([]byte(test.responses[readCount]))
				require.NoError(t, err)
				readCount++
			}))
			defer server.Close()
			client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
				sdkgo.ConnectionRef{Provider: "openai", Name: "default"}: {APIKey: sdkgo.NewSecretString("test-key")},
			})
			require.NoError(t, err)
			call := sdkgo.Call{Context: testsupport.NewDexContext("stream-flow", "read-back"), Connection: sdkgo.ConnectionRef{Provider: "openai", Name: "default"}, Operation: CreateResponseDefinition.Operation}
			streamBody := test.streamBody
			if streamBody == "" {
				streamBody = sse(`{"type":"response.created","response":{"id":"resp_known","status":"in_progress"}}`)
			}
			attempt := client.CreateResponse().readStream(call, newTestHeartbeat(call), strings.NewReader(streamBody), nil, "req_create")
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("stream-flow", "result"), streamAttemptOperation{attempt: attempt}, call.Connection, struct{}{})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, "resp_known", result.Value.ID)
			require.Equal(t, "resp_known", result.Receipt.ProviderObjectID)
			require.Equal(t, len(test.responses), readCount)
		})
	}
}

func TestOpenAIStreamReadsPastEventsThatDoNotEndIt(t *testing.T) {
	for _, test := range []struct {
		name       string
		events     []string
		wantBranch sdkgo.BranchID
		wantID     string
	}{
		{
			name: "error event with a known ID",
			events: []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`, `{"type":"error"}`,
				`{"type":"response.failed","response":{"id":"resp_1","status":"failed"}}`,
			},
			wantBranch: CreateResponseBranchFailed, wantID: "resp_1",
		},
		{
			name: "inconsistent later ID",
			events: []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
				`{"type":"response.in_progress","response":{"id":"resp other","status":"in_progress"}}`,
				`{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
			},
			wantBranch: CreateResponseBranchCompleted, wantID: "resp_1",
		},
		{
			name: "malformed event",
			events: []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`, `{"type":`,
				`{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
			},
			wantBranch: CreateResponseBranchCompleted, wantID: "resp_1",
		},
		{
			name: "terminal event naming another response",
			events: []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
				`{"type":"response.failed","response":{"id":"resp_other","status":"failed"}}`,
				`{"type":"response.completed","response":{"id":"resp_1","status":"completed"}}`,
			},
			wantBranch: CreateResponseBranchCompleted, wantID: "resp_1",
		},
		{
			name: "terminal event without an ID",
			events: []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
				`{"type":"response.completed","response":{"status":"completed"}}`,
				`{"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete"}}`,
			},
			wantBranch: CreateResponseBranchFailed, wantID: "resp_1",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runStreamAttempt(t, strings.NewReader(sse(test.events...)))
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantID, result.Value.ID)
		})
	}
}

// The pipe delivers the stream's end only while the client keeps reading.
func TestOpenAIStreamKeepsReadingItsResponseAfterAForeignTerminalEvent(t *testing.T) {
	for _, test := range []struct {
		name          string
		finalEvents   []string
		wantReadBacks int32
	}{
		{name: "own terminal event", finalEvents: []string{`{"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"content":[{"type":"output_text","text":"done"}]}]}}`}},
		{name: "end of stream", wantReadBacks: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var isStreamFinished atomic.Bool
			var readBacks atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, "/responses/resp_1", request.URL.Path)
				readBacks.Add(1)
				status := "cancelled"
				if isStreamFinished.Load() {
					status = "completed"
				}
				_, _ = fmt.Fprintf(response, `{"id":"resp_1","status":%q,"output":[{"content":[{"type":"output_text","text":"done"}]}]}`, status)
			}))
			defer server.Close()
			client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
				sdkgo.ConnectionRef{Provider: "openai", Name: "default"}: {APIKey: sdkgo.NewSecretString("test-key")},
			})
			require.NoError(t, err)
			events := []string{
				`{"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}`,
				`{"type":"response.completed","response":{"id":"resp_other","status":"completed"}}`,
			}
			for range 50 {
				events = append(events, `{"type":"response.output_text.delta","delta":"more"}`)
			}
			events = append(events, test.finalEvents...)
			body, writer := io.Pipe()
			t.Cleanup(func() { _ = body.Close() })
			go func() {
				for _, event := range events {
					if _, err := io.WriteString(writer, "data: "+event+"\n\n"); err != nil {
						return
					}
				}
				isStreamFinished.Store(true)
				_ = writer.Close()
			}()
			attemptContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			dexContext := testsupport.NewDexContext("stream-flow", "foreign-terminal")
			dexContext.Context = attemptContext
			call := sdkgo.Call{Context: dexContext, Connection: sdkgo.ConnectionRef{Provider: "openai", Name: "default"}, Operation: CreateResponseDefinition.Operation}
			attempt := client.CreateResponse().readStream(call, newTestHeartbeat(call), body, nil, "req_foreign")
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("stream-flow", "result"), streamAttemptOperation{attempt: attempt}, call.Connection, struct{}{})
			require.NoError(t, err, "a read-back before the stream ends finds the response cancelled")
			require.Equal(t, CreateResponseBranchCompleted, result.Branch)
			require.Equal(t, "resp_1", result.Value.ID)
			require.Equal(t, "done", result.Value.OutputText)
			require.Equal(t, test.wantReadBacks, readBacks.Load())
		})
	}
}

// drainObservingReader reports whether its reader reached EOF.
type drainObservingReader struct {
	reader  io.Reader
	isAtEOF bool
}

func (reader *drainObservingReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if errors.Is(err, io.EOF) {
		reader.isAtEOF = true
	}
	return count, err
}

func TestOpenAIOversizedStreamFinishesBeforeReadBack(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/responses/resp_long", request.URL.Path)
		_, _ = response.Write([]byte(`{"id":"resp_long","status":"completed","output":[{"content":[{"type":"output_text","text":"long"}]}]}`))
	}))
	defer server.Close()
	client, err := New(Config{Endpoint: server.URL, MaxResponseBytes: 512}, sdkgo.StaticCredentialProvider[Credentials]{
		sdkgo.ConnectionRef{Provider: "openai", Name: "default"}: {APIKey: sdkgo.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	events := []string{`{"type":"response.created","response":{"id":"resp_long","status":"in_progress"}}`}
	for range 20 {
		events = append(events, `{"type":"response.output_text.delta","delta":"more text"}`)
	}
	events = append(events, `{"type":"response.completed","response":{"id":"resp_long","status":"completed"}}`)
	body := &drainObservingReader{reader: strings.NewReader(sse(events...))}
	call := sdkgo.Call{Context: testsupport.NewDexContext("stream-flow", "oversized"), Connection: sdkgo.ConnectionRef{Provider: "openai", Name: "default"}, Operation: CreateResponseDefinition.Operation}
	attempt := client.CreateResponse().readStream(call, newTestHeartbeat(call), body, nil, "req_long")
	require.True(t, body.isAtEOF, "closing an unfinished stream would cancel the response")
	result, err := sdkgo.RunMutation(testsupport.NewDexContext("stream-flow", "result"), streamAttemptOperation{attempt: attempt}, call.Connection, struct{}{})
	require.NoError(t, err)
	require.Equal(t, CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, "long", result.Value.OutputText)
}

func runStreamAttempt(t *testing.T, body *strings.Reader) sdkgo.MutationResult[Response] {
	t.Helper()
	client := &Client{maxResponseBytes: 16 << 10, maxSSEEventBytes: 4 << 10}
	call := sdkgo.Call{Context: testsupport.NewDexContext("stream-flow", "stream-step")}
	attempt := CreateResponseOperation{client: client}.readStream(call, newTestHeartbeat(call), body, nil, "req_stream")
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("stream-flow", "stream-step"),
		streamAttemptOperation{attempt: attempt}, sdkgo.ConnectionRef{Provider: "openai", Name: "default"}, struct{}{},
	)
	require.NoError(t, err)
	return result
}

// newTestHeartbeat returns a heartbeat whose ticker never starts.
func newTestHeartbeat(call sdkgo.Call) *createResponseHeartbeat {
	return newCreateResponseHeartbeat(call, time.Hour)
}

func sse(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}
