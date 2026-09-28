//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claude_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/anthropic/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// TestLiveGenerateText makes one unbilled unknown-model request and one tiny structured generation.
func TestLiveGenerateText(t *testing.T) {
	apiKey := os.Getenv("CLAUDE_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("CLAUDE_CONNECTOR_TEST_API_KEY is not configured")
	}
	client, err := claude.New(claude.Config{
		Model: os.Getenv("CLAUDE_CONNECTOR_TEST_MODEL"), WorkspaceID: os.Getenv("CLAUDE_CONNECTOR_TEST_WORKSPACE_ID"),
	}, sdkgo.StaticCredentialProvider[claude.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
	require.NoError(t, err)
	requireNoAPIKey := func(t *testing.T, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// A boolean assertion keeps the key out of the failure message.
		require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
	}

	t.Run("unknown model is a conclusive rejection", func(t *testing.T) {
		request := userRequest("Hi")
		request.Model = "claude-connector-test-no-such-model"
		rejected, err := sdkgo.RunQuery(testsupport.NewDexContext("live-claude-flow", "live-unknown-model-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, rejected)
		require.Equal(t, claude.GenerateTextBranchProviderRejected, rejected.Branch)
		require.Equal(t, sdkgo.FailureNotFound, rejected.Failure.Kind, "an invalid API key is AUTHENTICATION: %s", rejected.Failure.Message)
	})

	t.Run("structured generation", func(t *testing.T) {
		request := userRequest(`Reply with {"ok": true}.`)
		request.StructuredOutput = &llm.StructuredOutput{Name: "probe", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
			"required": []any{"ok"}, "additionalProperties": false,
		}}
		request.MaxOutputTokens, request.ReasoningEffort = 2048, llm.ReasoningEffortLow
		generated, err := sdkgo.RunQuery(testsupport.NewDexContext("live-claude-flow", "live-generate-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, generated)
		if generated.Failure != nil && generated.Failure.Kind == sdkgo.FailureQuotaExhausted {
			t.Fatalf("the Claude organization has a billing issue or reached its spend cap (%s); fix it in the Claude Console to verify generation", generated.Failure.Message)
		}
		require.Equal(t, claude.GenerateTextBranchGenerated, generated.Branch, "failure: %+v", generated.Failure)
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
