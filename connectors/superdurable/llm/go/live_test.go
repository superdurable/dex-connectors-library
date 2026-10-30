//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestLiveGenerateText makes one tiny generation with each provider whose key
// LLM_CONNECTOR_TEST_<PROVIDER>_API_KEY holds, such as LLM_CONNECTOR_TEST_DEEPSEEK_API_KEY.
// LLM_CONNECTOR_TEST_<PROVIDER>_REGION and _MODEL optionally pick the region and model.
func TestLiveGenerateText(t *testing.T) {
	hasAnyKey := false
	for _, served := range servedProviders {
		environmentPrefix := "LLM_CONNECTOR_TEST_" + strings.ToUpper(string(served.provider)) + "_"
		apiKey := os.Getenv(environmentPrefix + "API_KEY")
		if apiKey == "" {
			continue
		}
		hasAnyKey = true
		t.Run(string(served.provider), func(t *testing.T) {
			config := llm.Config{
				Provider: served.provider, Region: llm.Region(os.Getenv(environmentPrefix + "REGION")), Model: os.Getenv(environmentPrefix + "MODEL"),
			}
			client, err := llm.New(config, sdkgo.StaticCredentialProvider[llm.Credentials]{
				routingTestConnection: {APIKey: sdkgo.NewSecretString(apiKey)},
			})
			require.NoError(t, err)
			ctx := testsupport.NewDexContext("live-llm-flow", fmt.Sprintf("live-%s-%d", served.provider, time.Now().UnixNano()))
			request := routingUserRequest("")
			request.Messages[0].Text = "Reply with the single word ok."
			result, err := sdkgo.RunQuery(ctx, client.GenerateText(), routingTestConnection, request)
			require.NoError(t, err)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			// A boolean assertion keeps the key out of the failure message.
			require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.NotEmpty(t, result.Value.Text)
			require.Positive(t, result.Value.Usage.TotalTokens)
			require.Equal(t, string(served.provider), result.Receipt.Provider)
			t.Logf("requestedModel=%s servedModel=%s finishReason=%s totalTokens=%d requestID=%s",
				result.Value.RequestedModel, result.Value.ServedModel, result.Value.ProviderFinishReason,
				result.Value.Usage.TotalTokens, result.Receipt.ProviderRequestID)
		})
	}
	if !hasAnyKey {
		t.Skip("no LLM_CONNECTOR_TEST_<PROVIDER>_API_KEY is configured")
	}
}
