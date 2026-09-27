//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mistral_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mistral"
	"github.com/superdurable/dex-connectors-library/connectors/mistral/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
)

// TestLiveGenerateText makes one unbilled unknown-model request and one tiny structured generation.
func TestLiveGenerateText(t *testing.T) {
	apiKey := os.Getenv("MISTRAL_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("MISTRAL_CONNECTOR_TEST_API_KEY is not configured")
	}
	client, err := mistral.New(mistral.Config{
		Model: os.Getenv("MISTRAL_CONNECTOR_TEST_MODEL"), Endpoint: os.Getenv("MISTRAL_CONNECTOR_TEST_ENDPOINT"),
	}, sdkgo.StaticCredentialProvider[mistral.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
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
		request.Model = "mistral-connector-test-no-such-model"
		rejected, err := sdkgo.RunQuery(testsupport.NewDexContext("live-mistral-flow", "live-unknown-model-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, rejected)
		require.Equal(t, mistral.GenerateTextBranchProviderRejected, rejected.Branch)
		require.Contains(t, []sdkgo.FailureKind{sdkgo.FailureNotFound, sdkgo.FailureProviderRejection}, rejected.Failure.Kind,
			"an invalid API key is AUTHENTICATION: %s", rejected.Failure.Message)
		t.Logf("unknown model failure: %s", rejected.Failure.Message)
	})

	t.Run("structured generation", func(t *testing.T) {
		request := userRequest(`Reply with {"ok": true}.`)
		request.StructuredOutput = &llm.StructuredOutput{Name: "probe", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
			"required": []any{"ok"}, "additionalProperties": false,
		}}
		request.MaxOutputTokens = 256
		generated, err := sdkgo.RunQuery(testsupport.NewDexContext("live-mistral-flow", "live-generate-step"),
			client.GenerateText(), testConnection, request)
		require.NoError(t, err)
		requireNoAPIKey(t, generated)
		if generated.Failure != nil && generated.Failure.Kind == sdkgo.FailureQuotaExhausted {
			t.Fatalf("the Mistral organization has a billing issue (%s); raise the spending limit in the Admin Panel to verify generation", generated.Failure.Message)
		}
		require.Equal(t, mistral.GenerateTextBranchGenerated, generated.Branch, "failure: %+v", generated.Failure)
		var answer struct {
			OK bool `json:"ok"`
		}
		require.NoError(t, json.Unmarshal([]byte(generated.Value.Text), &answer))
		require.True(t, answer.OK)
		require.Positive(t, generated.Value.Usage.TotalTokens)
		t.Logf("requestedModel=%s servedModel=%s finishReason=%s requestID=%s inputTokens=%d outputTokens=%d",
			generated.Value.RequestedModel, generated.Value.ServedModel, generated.Value.ProviderFinishReason,
			generated.Receipt.ProviderRequestID, generated.Value.Usage.InputTokens, generated.Value.Usage.OutputTokens)
	})
}
