//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/integrationtest/fixturellm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
)

func TestFixtureLLMConnectorRunsTheTextGenerationScenariosWithRealDex(t *testing.T) {
	llmtest.RunTextGenerationDexScenarios(t, &llmtest.TextGenerationDexScenarioSuite{
		Dialect: openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
			ConnectionModel: "fixture-model-a", AlternateModel: "fixture-reasoner-b", IsStreaming: true,
			QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "fixture_quota_exhausted",
			ContentPolicyErrorToken: "fixture_content_filter",
		}),
		ConnectorID:         fixturellm.ConnectorID,
		NewGenerateTextStep: newFixtureGenerateTextStep,
	})
}

func newFixtureGenerateTextStep(
	t testing.TB, connection llmtest.FakeConnection, config llmtest.DexScenarioStepConfig,
) sdkgo.QueryStep[llmtest.DexScenarioInput, llm.TextGenerationRequest, llm.TextGenerationResponse] {
	t.Helper()
	client, err := fixturellm.New(fixturellm.Config{
		Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[fixturellm.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	fixtureConnection, err := fixturellm.NewConnection(client, connection.Reference)
	require.NoError(t, err)
	return fixturellm.NewGenerateTextStep(fixturellm.GenerateTextStepConfig[llmtest.DexScenarioInput]{
		StepType: config.StepType, Annotations: config.Annotations, Connection: fixtureConnection,
		MapToOperationInput: config.MapToOperationInput,
		Generated:           config.Generated, Truncated: config.Truncated, Blocked: config.Blocked,
		ProviderRejected: config.ProviderRejected, InvalidResponse: config.InvalidResponse, Defect: config.Defect,
		TextStream: config.TextStream, StepOptionsOverride: config.StepOptionsOverride,
	})
}
