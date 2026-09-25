// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
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

func TestOpenAIStreamErrorEventIsFailed(t *testing.T) {
	result := runStreamAttempt(t, strings.NewReader(sse(`{"type":"error","sequence_number":1,"error":{"message":"secret provider body"}}`)))
	require.Equal(t, CreateResponseBranchFailed, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "secret provider body")
}

func TestOpenAIStreamEarlyEOFAndBoundsAreUnknown(t *testing.T) {
	for name, test := range map[string]struct {
		client *Client
		body   string
	}{
		"early eof": {
			client: &Client{maxResponseBytes: 1024, maxSSEEventBytes: 512},
			body:   sse(`{"type":"response.created","response":{"id":"resp_known"}}`),
		},
		"event limit": {
			client: &Client{maxResponseBytes: 1024, maxSSEEventBytes: 16},
			body:   sse(`{"type":"response.created","response":{"id":"resp_known"}}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			attempt := CreateResponseOperation{client: test.client}.readStream(sdkgo.Call{}, strings.NewReader(test.body), nil, "req_1")
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

func runStreamAttempt(t *testing.T, body *strings.Reader) sdkgo.MutationResult[Response] {
	t.Helper()
	client := &Client{maxResponseBytes: 16 << 10, maxSSEEventBytes: 4 << 10}
	attempt := CreateResponseOperation{client: client}.readStream(sdkgo.Call{}, body, nil, "req_stream")
	result, err := sdkgo.RunMutation(
		testsupport.NewDexContext("stream-flow", "stream-step"),
		streamAttemptOperation{attempt: attempt}, sdkgo.ConnectionRef{Provider: "openai", Name: "default"}, struct{}{},
	)
	require.NoError(t, err)
	return result
}

func sse(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}
