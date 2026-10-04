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
)

// TestLiveCreateResponse makes one unbilled unknown-model request and one tiny structured stored response.
func TestLiveCreateResponse(t *testing.T) {
	apiKey := os.Getenv("OPENAI_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_CONNECTOR_TEST_API_KEY is not configured")
	}
	client, err := openai.New(openai.Config{Model: os.Getenv("OPENAI_CONNECTOR_TEST_MODEL")},
		sdkgo.StaticCredentialProvider[openai.Credentials]{openAIConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
	require.NoError(t, err)
	requireNoAPIKey := func(t *testing.T, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// A boolean assertion keeps the key out of the failure message.
		require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
	}

	t.Run("unknown model is a conclusive rejection", func(t *testing.T) {
		rejected, err := sdkgo.RunMutation(testsupport.NewDexContext("live-openai-flow", "live-unknown-model-step"),
			client.CreateResponse(), openAIConnection, openai.CreateRequest{Model: "gpt-connector-test-no-such-model", Input: "Hi"})
		require.NoError(t, err)
		requireNoAPIKey(t, rejected)
		require.Equal(t, openai.CreateResponseBranchProviderRejected, rejected.Branch)
		require.NotEqual(t, sdkgo.FailureAuthentication, rejected.Failure.Kind, "the API key is invalid: %s", rejected.Failure.Message)
	})

	t.Run("structured response with the connection's model", func(t *testing.T) {
		created, err := sdkgo.RunMutation(testsupport.NewDexContext("live-openai-flow", "live-create-step"),
			client.CreateResponse(), openAIConnection, openai.CreateRequest{
				Input: `Reply with {"ok": true}.`,
				StructuredOutput: &openai.StructuredOutput{Name: "probe", Strict: true, Schema: map[string]any{
					"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
					"required": []any{"ok"}, "additionalProperties": false,
				}},
			})
		require.NoError(t, err)
		requireNoAPIKey(t, created)
		require.Equal(t, openai.CreateResponseBranchCompleted, created.Branch, "failure: %+v", created.Failure)
		var answer struct {
			OK bool `json:"ok"`
		}
		require.NoError(t, json.Unmarshal([]byte(created.Value.OutputText), &answer))
		require.True(t, answer.OK)
		require.Positive(t, created.Value.Usage.TotalTokens)
		t.Logf("responseID=%s model=%s status=%s inputTokens=%d outputTokens=%d",
			created.Value.ID, created.Value.Model, created.Value.Status, created.Value.Usage.InputTokens, created.Value.Usage.OutputTokens)
	})
}
