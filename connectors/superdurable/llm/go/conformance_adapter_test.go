// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

// Scenario name prefixes select a custom-endpoint connection; every other scenario uses openai.
const (
	anthropicCompatibleScenarioPrefix        = "anthropic-compatible-"
	openAICompatibleResponsesScenarioPrefix = "openai-compatible-responses-"
)

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "llm", Name: "llm-conformance"}
	client, err := llm.New(conformanceConnectionConfig(scenario.Name), sdkgo.StaticCredentialProvider[llm.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sk-llm-conformance-key")},
	}, llm.WithHTTPClient(transport), llm.WithBaseURLForTest(endpoint))
	require.NoError(t, err)
	context := testsupport.NewDexContext("llm-conformance-flow", "llm-conformance-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"generateText": newConformanceQueryInvocation(t, scenario.Input, context, client.GenerateText(), reference),
	}}
}

// conformanceConnectionConfig mirrors the Python and TypeScript adapters' scenario-name selection.
func conformanceConnectionConfig(scenarioName string) llm.Config {
	switch {
	case strings.HasPrefix(scenarioName, anthropicCompatibleScenarioPrefix):
		return llm.Config{
			Provider: llm.ProviderAnthropicCompatible, BaseURL: "https://gateway.example/anthropic", Model: "gateway-model",
		}
	case strings.HasPrefix(scenarioName, openAICompatibleResponsesScenarioPrefix):
		return llm.Config{
			Provider: llm.ProviderOpenaiCompatible, BaseURL: "https://gateway.example/v1", Model: "gateway-model",
			OpenAICompatibleAPI: llm.OpenAICompatibleAPIResponses,
		}
	default:
		return llm.Config{Provider: llm.ProviderOpenai, Model: "gpt-6-sol"}
	}
}
