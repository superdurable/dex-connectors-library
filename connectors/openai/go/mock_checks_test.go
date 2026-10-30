// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/openai/openaimock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/connectormock"
)

const completedResponseBody = `{"id":"resp_real","model":"gpt-6-sol","status":"completed","output":[],"usage":{}}`

func TestOpenAIMockRejectsTheSameRequestsAsTheRealConnector(t *testing.T) {
	var providerRequests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerRequests++
		_, _ = response.Write([]byte(completedResponseBody))
	}))
	defer server.Close()
	client := newClient(t, server.URL)
	mock := openaimock.New(t, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_scripted", Status: "completed", OutputText: "scripted"}, nil))

	rejected := map[string]openai.CreateRequest{
		"missing input":               {Model: "gpt-6-sol"},
		"model with a space":          {Model: "gpt 6", Input: "profile"},
		"input that is not JSON":      {Input: make(chan int)},
		"schema that is not JSON":     {Input: "profile", StructuredOutput: &openai.StructuredOutput{Name: "summary", Schema: map[string]any{"bad": make(chan int)}}},
		"model with a non-ASCII rune": {Model: "gpt-é", Input: "profile"},
		"model over 256 characters":   {Model: strings.Repeat("a", 257), Input: "profile"},
	}
	for name, request := range rejected {
		t.Run(name, func(t *testing.T) {
			realResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-real", "real-"+name), client.CreateResponse(), openAIConnection, request)
			require.NoError(t, err)
			mockResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-mock", "mock-"+name), mock.CreateResponse(), openAIConnection, request)
			require.NoError(t, err)
			require.Equal(t, openai.CreateResponseBranchDefect, realResult.Branch)
			require.Equal(t, realResult.Branch, mockResult.Branch)
			require.Equal(t, realResult.Failure, mockResult.Failure, "the mock reports the real connector's failure")
			require.Equal(t, sdkgo.FailureValidation, mockResult.Failure.Kind)
		})
	}
	require.Zero(t, providerRequests, "the real connector rejects these requests before dispatch")

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-mock", "mock-valid"), mock.CreateResponse(), openAIConnection, openai.CreateRequest{Model: " ", Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch, "a blank model uses the connection's model")
	require.Equal(t, "resp_scripted", result.Value.ID, "rejected requests do not consume scripted answers")
}

func TestOpenAIMockLeavesTheSchemaContentToOpenAI(t *testing.T) {
	var providerRequests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		providerRequests++
		_, _ = response.Write([]byte(completedResponseBody))
	}))
	defer server.Close()
	request := openai.CreateRequest{Input: "profile", StructuredOutput: &openai.StructuredOutput{Name: "not a portable name"}}

	realResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-real", "real-schema"), newClient(t, server.URL).CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, realResult.Branch, "the real connector forwards the schema to OpenAI")
	require.Equal(t, 1, providerRequests)

	mock := openaimock.New(t, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_json", Status: "completed", OutputText: `{"anything":1}`}, nil))
	mockResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-mock", "mock-schema"), mock.CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, mockResult.Branch, "the mock does not judge a schema that OpenAI judges")
}

func TestOpenAIMockRejectsTheDefaultAnswerForStructuredOutput(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-default"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, result.Failure.Kind)
	require.Len(t, recorder.failures(), 1)
	require.Contains(t, recorder.failures()[0], `default answer for branch "completed" does not match request`)
	for _, message := range []string{result.Failure.Message, recorder.failures()[0]} {
		require.Contains(t, message, "the request sets StructuredOutput, so a completed answer's outputText must be exactly one JSON value")
		require.Contains(t, message, "script JSON text with the mock's Default or Respond")
		require.Contains(t, message, "(the manifest default answers are plain text)")
	}
	require.NotContains(t, result.Failure.Message+recorder.failures()[0], "shipped", "the error omits the answer text")

	plain, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "plain-default"), mock.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, plain.Branch)
	require.Equal(t, "The connector shipped today.", plain.Value.OutputText, "a plain request keeps the default answer")
	require.Len(t, recorder.failures(), 1)

	mock.CreateResponse().Default(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_json", Status: "completed", OutputText: `{"status":"shipped"}`}, nil))
	scripted, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-scripted-default"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, scripted.Branch, "a JSON Default serves structured requests")
	require.Len(t, recorder.failures(), 1)
}

func TestOpenAIMockAcceptsAnEmptyAnswerOfAPlainRequestLikeTheRealConnector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(completedResponseBody))
	}))
	defer server.Close()
	request := openai.CreateRequest{Input: "profile"}
	realResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-real", "real-empty"), newClient(t, server.URL).CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, realResult.Branch)
	require.Empty(t, realResult.Value.OutputText)

	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_empty", Status: "completed"}, nil))
	mockResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-mock", "mock-empty"), mock.CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, mockResult.Branch)
	require.Empty(t, recorder.failures())
}

func TestOpenAIMockAcceptsAJSONAnswerThatMatchesThePortableSchema(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_json", Status: "completed", OutputText: " {\"status\":\"shipped\"}\n"}, nil))

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-json"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	require.Equal(t, " {\"status\":\"shipped\"}\n", result.Value.OutputText, "the mock returns the scripted text unchanged; JSON allows whitespace around a value")
	require.Empty(t, recorder.failures())
}

func TestOpenAIMockRejectsAnswersThatBreakThePortableSchema(t *testing.T) {
	for name, text := range map[string]string{
		"wrong type":         `{"status":123}`,
		"missing property":   `{}`,
		"undeclared member":  `{"status":"shipped","extra":true}`,
		"fenced Markdown":    "```json\n{\"status\":\"shipped\"}\n```",
		"text after JSON":    `{"status":"shipped"} done`,
		"plain text summary": `Shipped today.`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &recordingMockTB{TB: t}
			mock := openaimock.New(recorder, openAIConnection.Name)
			mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_bad", Status: "completed", OutputText: text}, nil))

			result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-bad"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
			require.NoError(t, err)
			require.Equal(t, openai.CreateResponseBranchDefect, result.Branch)
			require.Len(t, recorder.failures(), 1)
			require.Contains(t, recorder.failures()[0], "the request sets StructuredOutput")
			require.NotContains(t, result.Failure.Message+recorder.failures()[0], "shipped", "the error omits the answer text")
		})
	}
}

func TestOpenAIMockChecksOnlyJSONSyntaxWhenTheSchemaIsOutsideThePortableSubset(t *testing.T) {
	request := structuredStatusRequest()
	request.StructuredOutput.Schema = map[string]any{
		"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}, "note": map[string]any{"type": "string"}},
		"required": []any{"status"},
	}
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_optional", Status: "completed", OutputText: `{"status":"shipped"}`}, nil))

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-optional"), mock.CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch, "an optional property is valid for a schema that is not portable")
	require.Empty(t, recorder.failures())

	for index, text := range []string{"Shipped.", "```json\n{\"status\":\"shipped\"}\n```", `{"status":"shipped"} done`} {
		mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_text", Status: "completed", OutputText: text}, nil))
		result, err = sdkgo.RunMutation(testsupport.NewDexContext("flow-1", fmt.Sprintf("structured-text-%d", index)), mock.CreateResponse(), openAIConnection, request)
		require.NoError(t, err)
		require.Equal(t, openai.CreateResponseBranchDefect, result.Branch, "%q is not exactly one JSON value", text)
		require.Len(t, recorder.failures(), index+1)
	}
}

func TestOpenAIMockNamesTheSchemaWhenAStrictAnswerDoesNotMatchIt(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_bad", Status: "completed", OutputText: `{"status":123}`}, nil))

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-strict"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "the request sets StructuredOutput with Strict, so a completed answer's outputText must match its schema")
	require.Contains(t, result.Failure.Message, "script JSON text with the mock's Default or Respond")
}

func TestOpenAIMockChecksOnlyJSONSyntaxWithoutStrict(t *testing.T) {
	request := structuredStatusRequest()
	request.StructuredOutput.Strict = false
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_loose", Status: "completed", OutputText: `{"status":123}`}, nil))

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-loose"), mock.CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch, "OpenAI guarantees schema adherence only with Strict")
	require.Empty(t, recorder.failures())

	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_text", Status: "completed", OutputText: "Shipped."}, nil))
	result, err = sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-loose-text"), mock.CreateResponse(), openAIConnection, request)
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchDefect, result.Branch, "plain text never fits structured output")
	require.Len(t, recorder.failures(), 1)
}

// The answer check accepts what the real createResponse returns: a textless refusal and a failed incomplete Response.
func TestOpenAIMockDeliversWhatTheRealConnectorCompletesForStructuredOutput(t *testing.T) {
	for _, test := range []struct {
		name       string
		body       string
		wantBranch sdkgo.BranchID
		mockCase   func(openai.Response, *sdkgo.Failure) connectormock.Case[openai.Response]
	}{
		{
			name: "refusal", body: `{"id":"resp_refusal","model":"gpt-6-sol","status":"completed","output":[{"content":[{"type":"refusal","refusal":"I cannot help."}]}],"usage":{}}`,
			wantBranch: openai.CreateResponseBranchCompleted, mockCase: openaimock.CreateResponseCompleted,
		},
		{
			name: "incomplete", body: `{"id":"resp_cut","model":"gpt-6-sol","status":"incomplete","output":[{"content":[{"type":"output_text","text":"{\"status\":\"shi"}]}],"usage":{}}`,
			wantBranch: openai.CreateResponseBranchFailed, mockCase: openaimock.CreateResponseFailed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			realResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-real", "real-"+test.name), newClient(t, server.URL).CreateResponse(), openAIConnection, structuredStatusRequest())
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, realResult.Branch)

			recorder := &recordingMockTB{TB: t}
			mock := openaimock.New(recorder, openAIConnection.Name)
			mock.CreateResponse().Respond(test.mockCase(realResult.Value, realResult.Failure))
			mockResult, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-mock", "mock-"+test.name), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, mockResult.Branch)
			require.Equal(t, realResult.Value, mockResult.Value)
			require.Empty(t, recorder.failures())
		})
	}
}

func TestOpenAIMockRejectsACompletedAnswerWithAnotherStatus(t *testing.T) {
	for name, request := range map[string]openai.CreateRequest{"plain": {Input: "profile"}, "structured": structuredStatusRequest()} {
		t.Run(name, func(t *testing.T) {
			recorder := &recordingMockTB{TB: t}
			mock := openaimock.New(recorder, openAIConnection.Name)
			mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_cut", Status: "incomplete", OutputText: "Shipped"}, nil))
			result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "completed-incomplete-"+name), mock.CreateResponse(), openAIConnection, request)
			require.NoError(t, err)
			require.Equal(t, openai.CreateResponseBranchDefect, result.Branch, "the real connector selects failed for an incomplete Response")
			require.Len(t, recorder.failures(), 1)
			require.Contains(t, recorder.failures()[0], "CreateResponseFailed")
		})
	}
}

func TestOpenAIMockAcceptsACompletedPlainAnswerWithoutStatus(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{ID: "resp_plain", OutputText: "Shipped."}, nil))
	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "completed-no-status"), mock.CreateResponse(), openAIConnection, openai.CreateRequest{Input: "profile"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, result.Branch)
	require.Empty(t, recorder.failures())
}

func TestOpenAIMockChecksAScriptedAnswerWithoutStatus(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseCompleted(openai.Response{OutputText: "Shipped."}, nil))

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-no-status"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchDefect, result.Branch, "a real Response always has a status, so an unset one is still checked")
	require.Len(t, recorder.failures(), 1)
}

func TestOpenAIMockDoesNotCheckAnswersOnOtherBranches(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := openaimock.New(recorder, openAIConnection.Name)
	mock.CreateResponse().Respond(openaimock.CreateResponseIncompleteSummary())

	result, err := sdkgo.RunMutation(testsupport.NewDexContext("flow-1", "structured-incomplete"), mock.CreateResponse(), openAIConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchFailed, result.Branch, "an incomplete response carries partial text")
	require.Empty(t, recorder.failures())
}

func structuredStatusRequest() openai.CreateRequest {
	return openai.CreateRequest{Input: "Summarize the release.", StructuredOutput: &openai.StructuredOutput{
		Name: "summary", Strict: true, Schema: map[string]any{
			"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}},
			"required": []any{"status"}, "additionalProperties": false,
		},
	}}
}

type recordingMockTB struct {
	testing.TB
	mu       sync.Mutex
	messages []string
}

func (recorder *recordingMockTB) Errorf(format string, values ...any) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.messages = append(recorder.messages, fmt.Sprintf(format, values...))
}

func (recorder *recordingMockTB) failures() []string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]string(nil), recorder.messages...)
}
