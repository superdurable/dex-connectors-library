//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestLiveGenerateText makes one tiny generation with each provider whose key is in the environment.
func TestLiveGenerateText(t *testing.T) {
	keys := map[llmrouter.Provider]string{
		llmrouter.ProviderOpenAI:    os.Getenv("LLM_CONNECTOR_TEST_OPENAI_API_KEY"),
		llmrouter.ProviderAnthropic: os.Getenv("LLM_CONNECTOR_TEST_ANTHROPIC_API_KEY"),
		llmrouter.ProviderGemini:    os.Getenv("LLM_CONNECTOR_TEST_GEMINI_API_KEY"),
	}
	credentials := llmrouter.Credentials{
		OpenAIAPIKey:    sdkgo.NewSecretString(keys[llmrouter.ProviderOpenAI]),
		AnthropicAPIKey: sdkgo.NewSecretString(keys[llmrouter.ProviderAnthropic]),
		GeminiAPIKey:    sdkgo.NewSecretString(keys[llmrouter.ProviderGemini]),
	}
	providers := []llmrouter.Provider{llmrouter.ProviderOpenAI, llmrouter.ProviderAnthropic, llmrouter.ProviderGemini}
	for _, provider := range providers {
		if keys[provider] != "" {
			credentials.AuthMethodIDs = append(credentials.AuthMethodIDs, string(provider))
		}
	}
	client, err := llmrouter.New(llmrouter.Config{}, staticCredentials(credentials))
	require.NoError(t, err)
	hasAnyKey := false
	for _, provider := range providers {
		apiKey := keys[provider]
		if apiKey == "" {
			continue
		}
		hasAnyKey = true
		t.Run(string(provider), func(t *testing.T) {
			ctx := testsupport.NewDexContext("live-llm-flow", fmt.Sprintf("live-%s-%d", provider, time.Now().UnixNano()))
			result, err := sdkgo.RunQuery(ctx, client.GenerateText(), testConnection, userRequest(string(provider), "Reply with the single word ok."))
			require.NoError(t, err)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			for _, key := range keys {
				// A boolean assertion keeps the key out of the failure message.
				require.False(t, key != "" && strings.Contains(string(encoded), key), "a Result contains an API key")
			}
			require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.NotEmpty(t, result.Value.Text)
			require.Positive(t, result.Value.Usage.TotalTokens)
			t.Logf("model=%s servedModel=%s finishReason=%s totalTokens=%d",
				llmrouter.QualifiedModel(result), result.Value.ServedModel, result.Value.ProviderFinishReason, result.Value.Usage.TotalTokens)
		})
	}
	if !hasAnyKey {
		t.Skip("no LLM_CONNECTOR_TEST_*_API_KEY is configured")
	}
}
