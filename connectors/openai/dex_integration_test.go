//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

// TestGenerateTextRunsTheTextGenerationScenariosWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestGenerateTextRunsTheTextGenerationScenariosWithRealDex(t *testing.T) {
	llmtest.RunTextGenerationDexScenarios(t, &llmtest.TextGenerationDexScenarioSuite{
		Dialect: responsesDialect, ConnectorID: openai.ConnectorID, NewGenerateTextStep: newOpenAIGenerateTextStep,
	})
}

func newOpenAIGenerateTextStep(
	t testing.TB, connection llmtest.FakeConnection, config llmtest.DexScenarioStepConfig,
) sdkgo.QueryStep[llmtest.DexScenarioInput, llm.TextGenerationRequest, llm.TextGenerationResponse] {
	t.Helper()
	openAIConnection, err := openai.NewConnection(newFakeProviderClient(t, connection), connection.Reference)
	require.NoError(t, err)
	return openai.NewGenerateTextStep(openai.GenerateTextStepConfig[llmtest.DexScenarioInput]{
		StepType: config.StepType, Annotations: config.Annotations, Connection: openAIConnection,
		MapToOperationInput: config.MapToOperationInput,
		Generated:           config.Generated, Truncated: config.Truncated, Blocked: config.Blocked,
		ProviderRejected: config.ProviderRejected, InvalidResponse: config.InvalidResponse, Defect: config.Defect,
		TextStream: config.TextStream, StepOptionsOverride: config.StepOptionsOverride,
	})
}
