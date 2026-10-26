// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestMockRequestCheckMatchesLocalValidation(t *testing.T) {
	request := gemini.GenerateContentRequest{Contents: []gemini.Content{{Role: "user", Parts: []gemini.Part{{Text: "Summarize the launch."}}}}}
	require.Nil(t, gemini.ValidateGenerateContentMockRequest(request))
	request.Contents = nil
	failure := gemini.ValidateGenerateContentMockRequest(request)
	require.NotNil(t, failure)
	require.Equal(t, sdkgo.FailureValidation, failure.Kind)
	require.Equal(t, "at least one content is required", failure.Message)
}

func TestMockAnswerCheckRejectsStructuredMismatches(t *testing.T) {
	request := gemini.GenerateContentRequest{ResponseJSONSchema: map[string]any{
		"type": "object", "properties": map[string]any{"headline": map[string]any{"type": "string"}},
		"required": []any{"headline"}, "additionalProperties": false,
	}}
	answer := gemini.GenerateContentResponse{FinishReason: "STOP", Text: `{"headline":"Connector ships"}`}
	require.NoError(t, gemini.ValidateGenerateContentMockAnswer(request, answer))
	answer.Text = "plain text"
	require.ErrorContains(t, gemini.ValidateGenerateContentMockAnswer(request, answer), "exactly one JSON value")
	answer.Text = `{"unexpected":"value"}`
	require.ErrorContains(t, gemini.ValidateGenerateContentMockAnswer(request, answer), "portable schema")
	answer.Text = `{"headline":"Connector ships"}`
	answer.FinishReason = "MAX_TOKENS"
	require.ErrorContains(t, gemini.ValidateGenerateContentMockAnswer(request, answer), "STOP")
}
