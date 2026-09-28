//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package grok_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	grok "github.com/superdurable/dex-connectors-library/connectors/xai"
	"github.com/superdurable/dex-connectors-library/connectors/xai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// TestLiveGenerateText makes one unbilled unknown-model request and one tiny structured generation.
func TestLiveGenerateText(t *testing.T) {
	apiKey := os.Getenv("XAI_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("XAI_CONNECTOR_TEST_API_KEY is not configured")
	}
	client, err := grok.New(grok.Config{
		Model: os.Getenv("XAI_CONNECTOR_TEST_MODEL"), Endpoint: os.Getenv("XAI_CONNECTOR_TEST_ENDPOINT"),
	}, sdkgo.StaticCredentialProvider[grok.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
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
		request.Model = "grok-connector-test-no-such-model"
		rejected, err := sdkgo.RunQuery(testsupport.NewDexContext("live-grok-flow", "live-unknown-model-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, rejected)
		require.Equal(t, grok.GenerateTextBranchProviderRejected, rejected.Branch)
		require.Equal(t, sdkgo.FailureNotFound, rejected.Failure.Kind, "an incorrect API key is a 400 invalid-argument: %s", rejected.Failure.Message)
	})

	t.Run("structured generation", func(t *testing.T) {
		request := userRequest(`Reply with {"ok": true}.`)
		request.StructuredOutput = &llm.StructuredOutput{Name: "probe", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
			"required": []any{"ok"}, "additionalProperties": false,
		}}
		// ReasoningEffort stays unset: Grok 4.20 and Grok Build accept none, so any XAI_CONNECTOR_TEST_MODEL runs.
		request.MaxOutputTokens = 64
		generated, err := sdkgo.RunQuery(testsupport.NewDexContext("live-grok-flow", "live-generate-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, generated)
		if generated.Branch == grok.GenerateTextBranchProviderRejected {
			t.Fatalf("xAI rejected the request (%s); a team without prepaid credits is rejected until it buys credits", generated.Failure.Message)
		}
		require.Equal(t, grok.GenerateTextBranchGenerated, generated.Branch, "failure: %+v", generated.Failure)
		var answer struct {
			OK bool `json:"ok"`
		}
		require.NoError(t, json.Unmarshal([]byte(generated.Value.Text), &answer))
		require.True(t, answer.OK)
		require.Positive(t, generated.Value.Usage.TotalTokens)
		require.GreaterOrEqual(t, generated.Value.Usage.OutputTokens, generated.Value.Usage.ReasoningTokens,
			"OutputTokens includes ReasoningTokens under the llm.Usage contract")
		t.Logf("requestedModel=%s servedModel=%s finishReason=%s inputTokens=%d outputTokens=%d reasoningTokens=%d totalTokens=%d",
			generated.Value.RequestedModel, generated.Value.ServedModel, generated.Value.ProviderFinishReason,
			generated.Value.Usage.InputTokens, generated.Value.Usage.OutputTokens, generated.Value.Usage.ReasoningTokens,
			generated.Value.Usage.TotalTokens)
	})
}
