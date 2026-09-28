//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deepseek_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

// TestGenerateTextRunsTheTextGenerationScenariosWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestGenerateTextRunsTheTextGenerationScenariosWithRealDex(t *testing.T) {
	llmtest.RunTextGenerationDexScenarios(t, &llmtest.TextGenerationDexScenarioSuite{
		Dialect: deepSeekDialect, ConnectorID: deepseek.ConnectorID, NewGenerateTextStep: newDeepSeekGenerateTextStep,
	})
}

func newDeepSeekGenerateTextStep(
	t testing.TB, connection llmtest.FakeConnection, config llmtest.DexScenarioStepConfig,
) sdkgo.QueryStep[llmtest.DexScenarioInput, llm.TextGenerationRequest, llm.TextGenerationResponse] {
	t.Helper()
	deepSeekConnection, err := deepseek.NewConnection(newFakeProviderClient(t, connection), connection.Reference)
	require.NoError(t, err)
	return deepseek.NewGenerateTextStep(deepseek.GenerateTextStepConfig[llmtest.DexScenarioInput]{
		StepType: config.StepType, Annotations: config.Annotations, Connection: deepSeekConnection,
		MapToOperationInput: config.MapToOperationInput,
		Generated:           config.Generated, Truncated: config.Truncated, Blocked: config.Blocked,
		ProviderRejected: config.ProviderRejected, InvalidResponse: config.InvalidResponse, Defect: config.Defect,
		TextStream: config.TextStream, StepOptionsOverride: config.StepOptionsOverride,
	})
}
