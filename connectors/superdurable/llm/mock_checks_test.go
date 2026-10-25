// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/llmmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

func TestLLMMockRejectsTheSameNonPortableRequestAsTheRealConnector(t *testing.T) {
	request := routingUserRequest("")
	request.StructuredOutput = &textgen.StructuredOutput{Name: "summary", Schema: map[string]any{
		"type": "object", "properties": map[string]any{
			"status": map[string]any{"type": "string"}, "confidence": map[string]any{"type": "number"},
		}, "required": []any{"status", "confidence"},
	}}
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, routingTestAPIKey)
	client, err := llm.New(llm.Config{Provider: llm.ProviderOpenai, Model: "gpt-6-sol"}, routingCredentials(), llm.WithBaseURLForTest(provider.BaseURL()))
	require.NoError(t, err)
	realResult, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-real-invalid", "generate"), client.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)
	require.Empty(t, provider.Requests(), "the real connector rejects the request before dispatch")

	mock := llmmock.New(t, routingTestConnection.Name)
	mock.GenerateText().Respond(llmmock.GenerateTextStructuredAnswer())
	mockResult, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-invalid", "generate"), mock.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchDefect, realResult.Branch)
	require.Equal(t, realResult.Branch, mockResult.Branch)
	require.Equal(t, realResult.Failure.Kind, mockResult.Failure.Kind)
	require.Equal(t, realResult.Failure.Message, mockResult.Failure.Message)
	require.Equal(t, sdkgo.FailureValidation, mockResult.Failure.Kind)
	require.Contains(t, mockResult.Failure.Message, `additionalProperties" to false`)

	request.StructuredOutput.Schema["additionalProperties"] = false
	validResult, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-valid", "generate"), mock.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchGenerated, validResult.Branch, "invalid requests do not consume scripted answers")
}

func TestLLMMockChecksOnlyThePortableRulesWhenTheRealConnectorRejectsTheModelIDFirst(t *testing.T) {
	request := structuredStatusRequest()
	request.Model = "bad model"
	delete(request.StructuredOutput.Schema, "additionalProperties")
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, routingTestAPIKey)
	client, err := llm.New(llm.Config{Provider: llm.ProviderOpenai}, routingCredentials(), llm.WithBaseURLForTest(provider.BaseURL()))
	require.NoError(t, err)
	realResult, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-real-model-first", "generate"), client.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)

	mock := llmmock.New(t, routingTestConnection.Name)
	mockResult, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-model-first", "generate"), mock.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)

	require.Equal(t, llm.GenerateTextBranchDefect, realResult.Branch)
	require.Equal(t, llm.GenerateTextBranchDefect, mockResult.Branch)
	require.NotContains(t, realResult.Failure.Message, "additionalProperties", "the real connector rejects the model ID first")
	require.Contains(t, mockResult.Failure.Message, `additionalProperties" to false`)
}

func TestLLMMockRejectsScriptedAnswerThatDoesNotMatchSchema(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := llmmock.New(recorder, routingTestConnection.Name)
	mock.GenerateText().Respond(llmmock.GenerateTextGenerated(llm.GenerateTextResponse{Text: `{"status":123}`}, nil))
	request := structuredStatusRequest()

	result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-mismatch", "generate"), mock.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "does not match request")
	require.NotEmpty(t, recorder.failures())
	require.Contains(t, recorder.failures()[0], "scripted answer for branch \"generated\" does not match request")
}

func TestLLMMockFailsAStructuredRequestAnsweredByThePlainTextDefault(t *testing.T) {
	recorder := &recordingMockTB{TB: t}
	mock := llmmock.New(recorder, routingTestConnection.Name)

	result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-default-structured", "generate"), mock.GenerateText(), routingTestConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, result.Failure.Kind)
	require.Len(t, recorder.failures(), 1)
	for _, message := range []string{recorder.failures()[0], result.Failure.Message} {
		require.Contains(t, message, "the answer, scripted or the manifest default, does not match the request's structured output schema")
		require.Contains(t, message, "the test must script a JSON answer for that schema")
		require.Contains(t, message, "llmmock.GenerateTextGenerated(")
		require.NotContains(t, message, "The connector shipped on time.", "errors never repeat the answer text")
	}
}

func TestLLMMockRejectsAnswersTheRealOperationDoesNotReturn(t *testing.T) {
	plainRequest := routingUserRequest("")
	for _, testCase := range []struct {
		name           string
		request        llm.GenerateTextRequest
		answerText     string
		expectedReason string
	}{
		{"empty text for a plain request", plainRequest, "", "the answer has no text"},
		{"empty text for a structured request", structuredStatusRequest(), "", "the answer has no text"},
		{"a fenced JSON answer", structuredStatusRequest(), "```json\n{\"status\":\"shipped\"}\n```", "Markdown fence or surrounding whitespace"},
		{"a JSON answer with surrounding whitespace", structuredStatusRequest(), "{\"status\":\"shipped\"}\n", "Markdown fence or surrounding whitespace"},
		{"a fenced answer that does not match the schema", structuredStatusRequest(), "```json\n{\"status\":123}\n```", "does not match the request's structured output schema"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := &recordingMockTB{TB: t}
			mock := llmmock.New(recorder, routingTestConnection.Name)
			mock.GenerateText().Respond(llmmock.GenerateTextGenerated(llm.GenerateTextResponse{Text: testCase.answerText}, nil))

			result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-real-answer", "generate"), mock.GenerateText(), routingTestConnection, testCase.request)
			require.NoError(t, err)
			require.Equal(t, llm.GenerateTextBranchDefect, result.Branch)
			require.Len(t, recorder.failures(), 1)
			require.Contains(t, recorder.failures()[0], testCase.expectedReason)
			require.Contains(t, result.Failure.Message, testCase.expectedReason)
		})
	}
}

func TestLLMMockAcceptsTheExactTextTheRealOperationReturns(t *testing.T) {
	mock := llmmock.New(t, routingTestConnection.Name)
	mock.GenerateText().Respond(llmmock.GenerateTextGenerated(llm.GenerateTextResponse{Text: `{"status":"shipped"}`}, nil))

	structured, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-exact-structured", "generate"), mock.GenerateText(), routingTestConnection, structuredStatusRequest())
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchGenerated, structured.Branch)
	require.Equal(t, `{"status":"shipped"}`, structured.Value.Text)

	plain, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-mock-default-plain", "generate"), mock.GenerateText(), routingTestConnection, routingUserRequest(""))
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchGenerated, plain.Branch)
	require.Equal(t, "The connector shipped on time.", plain.Value.Text)
}

func structuredStatusRequest() llm.GenerateTextRequest {
	request := routingUserRequest("")
	request.StructuredOutput = &textgen.StructuredOutput{Name: "summary", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}},
		"required": []any{"status"}, "additionalProperties": false,
	}}
	return request
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
