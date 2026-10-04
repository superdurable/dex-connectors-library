//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

// TestGenerateTextRunsTheTextGenerationScenariosForEveryProviderWithRealDex uses the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func TestGenerateTextRunsTheTextGenerationScenariosForEveryProviderWithRealDex(t *testing.T) {
	for _, served := range servedProviders {
		t.Run(string(served.provider), func(t *testing.T) {
			textgentest.RunTextGenerationDexScenarios(t, &textgentest.TextGenerationDexScenarioSuite{
				Dialect: served.dialect, ConnectorID: llm.ConnectorID,
				NewGenerateTextStep: func(
					t testing.TB, connection textgentest.FakeConnection, config textgentest.DexScenarioStepConfig,
				) sdkgo.QueryStep[textgentest.DexScenarioInput, textgen.TextGenerationRequest, textgen.TextGenerationResponse] {
					return newScenarioGenerateTextStep(t, served.provider, connection, config)
				},
			})
		})
	}
}

// newScenarioGenerateTextStep builds the generated Step on a connection to provider's fake API.
func newScenarioGenerateTextStep(
	t testing.TB, provider llm.Provider, connection textgentest.FakeConnection, config textgentest.DexScenarioStepConfig,
) sdkgo.QueryStep[textgentest.DexScenarioInput, textgen.TextGenerationRequest, textgen.TextGenerationResponse] {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: provider, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	llmConnection, err := llm.NewConnection(client, connection.Reference)
	require.NoError(t, err)
	return llm.NewGenerateTextStep(llm.GenerateTextStepConfig[textgentest.DexScenarioInput]{
		StepType: config.StepType, Annotations: config.Annotations, Connection: llmConnection, ConnectionName: connection.Reference.Name,
		MapToOperationInput: config.MapToOperationInput,
		Generated:           config.Generated, Truncated: config.Truncated, Blocked: config.Blocked,
		ProviderRejected: config.ProviderRejected, InvalidResponse: config.InvalidResponse, Defect: config.Defect,
		TextStream: config.TextStream, StepOptionsOverride: config.StepOptionsOverride,
	})
}
