//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kimi_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi"
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// TestLiveGenerateText makes one unbilled unknown-model request and one tiny structured generation.
func TestLiveGenerateText(t *testing.T) {
	apiKey := os.Getenv("KIMI_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("KIMI_CONNECTOR_TEST_API_KEY is not configured")
	}
	client, err := kimi.New(kimi.Config{
		Model: os.Getenv("KIMI_CONNECTOR_TEST_MODEL"), Endpoint: os.Getenv("KIMI_CONNECTOR_TEST_ENDPOINT"),
	}, sdkgo.StaticCredentialProvider[kimi.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
	require.NoError(t, err)
	requireNoLiveAPIKey := func(t *testing.T, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// A boolean assertion keeps the key out of the failure message.
		require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
	}

	t.Run("unknown model is a conclusive rejection", func(t *testing.T) {
		request := userRequest("Hi")
		request.Model = "kimi-connector-test-no-such-model"
		rejected, err := sdkgo.RunQuery(testsupport.NewDexContext("live-kimi-flow", "live-unknown-model-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoLiveAPIKey(t, rejected)
		require.Equal(t, kimi.GenerateTextBranchProviderRejected, rejected.Branch)
		require.Equal(t, sdkgo.FailureNotFound, rejected.Failure.Kind,
			"a key from the other platform is AUTHENTICATION; set KIMI_CONNECTOR_TEST_ENDPOINT: %s", rejected.Failure.Message)
	})

	t.Run("structured generation", func(t *testing.T) {
		request := userRequest(`Reply with {"ok": true}.`)
		request.StructuredOutput = &llm.StructuredOutput{Name: "probe", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
			"required": []any{"ok"}, "additionalProperties": false,
		}}
		// Kimi recommends at least 16000 tokens for thinking models, because reasoning counts toward the limit.
		request.MaxOutputTokens = 16384
		generated, err := sdkgo.RunQuery(testsupport.NewDexContext("live-kimi-flow", "live-generate-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoLiveAPIKey(t, generated)
		if generated.Failure != nil && generated.Failure.Kind == sdkgo.FailureQuotaExhausted {
			t.Fatalf("the Kimi account balance is exhausted (%s); top up in the Kimi API Platform console to verify generation", generated.Failure.Message)
		}
		require.Equal(t, kimi.GenerateTextBranchGenerated, generated.Branch, "failure: %+v", generated.Failure)
		var answer struct {
			OK bool `json:"ok"`
		}
		require.NoError(t, json.Unmarshal([]byte(generated.Value.Text), &answer))
		require.True(t, answer.OK)
		require.Positive(t, generated.Value.Usage.TotalTokens)
		t.Logf("requestedModel=%s servedModel=%s finishReason=%s inputTokens=%d cachedInputTokens=%d outputTokens=%d",
			generated.Value.RequestedModel, generated.Value.ServedModel, generated.Value.ProviderFinishReason,
			generated.Value.Usage.InputTokens, generated.Value.Usage.CachedInputTokens, generated.Value.Usage.OutputTokens)
	})
}
