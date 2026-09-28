//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// TestLiveGenerateText makes one unbilled unknown-model request and one tiny structured generation.
func TestLiveGenerateText(t *testing.T) {
	apiKey := os.Getenv("OPENAI_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_CONNECTOR_TEST_API_KEY is not configured")
	}
	client, err := openai.New(openai.Config{Model: os.Getenv("OPENAI_CONNECTOR_TEST_MODEL")},
		sdkgo.StaticCredentialProvider[openai.Credentials]{generateTextConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
	require.NoError(t, err)
	requireNoAPIKey := func(t *testing.T, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// A boolean assertion keeps the key out of the failure message.
		require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
	}

	t.Run("unknown model is a conclusive rejection", func(t *testing.T) {
		request := generateTextUserRequest("Hi")
		request.Model = "gpt-connector-test-no-such-model"
		rejected, err := sdkgo.RunQuery(testsupport.NewDexContext("live-openai-flow", "live-unknown-model-step"),
			client.GenerateText(), generateTextConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, rejected)
		require.Equal(t, openai.GenerateTextBranchProviderRejected, rejected.Branch)
		require.NotEqual(t, sdkgo.FailureAuthentication, rejected.Failure.Kind, "the API key is invalid: %s", rejected.Failure.Message)
	})

	t.Run("structured generation", func(t *testing.T) {
		request := generateTextUserRequest(`Reply with {"ok": true}.`)
		request.StructuredOutput = &llm.StructuredOutput{Name: "probe", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
			"required": []any{"ok"}, "additionalProperties": false,
		}}
		request.MaxOutputTokens, request.ReasoningEffort = 1024, llm.ReasoningEffortLow
		generated, err := sdkgo.RunQuery(testsupport.NewDexContext("live-openai-flow", "live-generate-step"),
			client.GenerateText(), generateTextConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, generated)
		if generated.Failure != nil && generated.Failure.Kind == sdkgo.FailureQuotaExhausted {
			t.Fatalf("the OpenAI project has no credits or reached a spend limit (%s); fix billing to verify generation", generated.Failure.Message)
		}
		require.Equal(t, openai.GenerateTextBranchGenerated, generated.Branch, "failure: %+v", generated.Failure)
		var answer struct {
			OK bool `json:"ok"`
		}
		require.NoError(t, json.Unmarshal([]byte(generated.Value.Text), &answer))
		require.True(t, answer.OK)
		require.Positive(t, generated.Value.Usage.TotalTokens)
		t.Logf("requestedModel=%s servedModel=%s finishReason=%s inputTokens=%d outputTokens=%d reasoningTokens=%d",
			generated.Value.RequestedModel, generated.Value.ServedModel, generated.Value.ProviderFinishReason,
			generated.Value.Usage.InputTokens, generated.Value.Usage.OutputTokens, generated.Value.Usage.ReasoningTokens)
	})
}
