//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mistral_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mistral"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

// TestGenerateTextRunsTheTextGenerationScenariosWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestGenerateTextRunsTheTextGenerationScenariosWithRealDex(t *testing.T) {
	llmtest.RunTextGenerationDexScenarios(t, &llmtest.TextGenerationDexScenarioSuite{
		Dialect: mistralDialect, ConnectorID: mistral.ConnectorID, NewGenerateTextStep: newMistralGenerateTextStep,
	})
}

func newMistralGenerateTextStep(
	t testing.TB, connection llmtest.FakeConnection, config llmtest.DexScenarioStepConfig,
) sdkgo.QueryStep[llmtest.DexScenarioInput, llm.TextGenerationRequest, llm.TextGenerationResponse] {
	t.Helper()
	mistralConnection, err := mistral.NewConnection(newFakeProviderClient(t, connection), connection.Reference)
	require.NoError(t, err)
	return mistral.NewGenerateTextStep(mistral.GenerateTextStepConfig[llmtest.DexScenarioInput]{
		StepType: config.StepType, Annotations: config.Annotations, Connection: mistralConnection,
		MapToOperationInput: config.MapToOperationInput,
		Generated:           config.Generated, Truncated: config.Truncated, Blocked: config.Blocked,
		ProviderRejected: config.ProviderRejected, InvalidResponse: config.InvalidResponse, Defect: config.Defect,
		TextStream: config.TextStream, StepOptionsOverride: config.StepOptionsOverride,
	})
}
