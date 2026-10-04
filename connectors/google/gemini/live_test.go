//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestLiveGenerateContent makes one unbilled unknown-model request and one tiny structured generation.
func TestLiveGenerateContent(t *testing.T) {
	apiKey := os.Getenv("GEMINI_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("GEMINI_CONNECTOR_TEST_API_KEY is not configured")
	}
	model := os.Getenv("GEMINI_CONNECTOR_TEST_MODEL")
	if model == "" {
		model = liveDefaultModel
	}
	client, err := gemini.New(gemini.Config{}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		geminiConnection: {APIKey: sdkgo.NewSecretString(apiKey)},
	})
	require.NoError(t, err)
	requireNoAPIKey := func(t *testing.T, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// A boolean assertion keeps the key out of the failure message.
		require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
	}

	t.Run("unknown model is a conclusive rejection", func(t *testing.T) {
		rejected, err := sdkgo.RunQuery(
			testsupport.NewDexContext("live-gemini-flow", "live-unknown-model-step"), client.GenerateContent(), geminiConnection,
			gemini.GenerateContentRequest{Model: "gemini-connector-test-no-such-model", Contents: userPrompt("Hi")},
		)
		require.NoError(t, err)
		require.Equal(t, gemini.GenerateContentBranchProviderRejected, rejected.Branch)
		require.Equal(t, sdkgo.FailureNotFound, rejected.Failure.Kind, "an invalid API key would be AUTHENTICATION: %s", rejected.Failure.Message)
		requireNoAPIKey(t, rejected)
		t.Logf("branch=%s failure=%q", rejected.Branch, rejected.Failure.Message)
	})

	t.Run("structured generation", func(t *testing.T) {
		// Keeps Google's recommended Gemini 3 defaults; MaxOutputTokens includes thought tokens, so it leaves headroom.
		generated, err := sdkgo.RunQuery(
			testsupport.NewDexContext("live-gemini-flow", "live-generate-step"), client.GenerateContent(), geminiConnection,
			gemini.GenerateContentRequest{
				Model:              model,
				SystemInstruction:  "Answer with JSON only.",
				Contents:           userPrompt(`Return {"ok": true}.`),
				ResponseJSONSchema: map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []any{"ok"}},
				MaxOutputTokens:    1024,
			},
		)
		require.NoError(t, err)
		requireNoAPIKey(t, generated)
		if generated.Failure != nil && strings.HasPrefix(generated.Failure.Message, "provider returned HTTP 402") {
			t.Fatalf("the Gemini project has no prepay credits (%s); add credits in Google AI Studio to verify generation", generated.Failure.Message)
		}
		require.Equal(t, gemini.GenerateContentBranchGenerated, generated.Branch, "failure: %+v", generated.Failure)
		var answer struct {
			OK bool `json:"ok"`
		}
		require.NoError(t, json.Unmarshal([]byte(generated.Value.Text), &answer), "responseJsonSchema must yield JSON")
		require.True(t, answer.OK)
		require.Equal(t, "STOP", generated.Value.FinishReason)
		require.NotEmpty(t, generated.Value.ModelVersion)
		require.Positive(t, generated.Value.Usage.TotalTokens)
		t.Logf("branch=%s model=%s modelVersion=%s finishReason=%s promptTokens=%d candidateTokens=%d thoughtsTokens=%d totalTokens=%d",
			generated.Branch, model, generated.Value.ModelVersion, generated.Value.FinishReason, generated.Value.Usage.PromptTokens,
			generated.Value.Usage.CandidateTokens, generated.Value.Usage.ThoughtsTokens, generated.Value.Usage.TotalTokens)
	})
}
