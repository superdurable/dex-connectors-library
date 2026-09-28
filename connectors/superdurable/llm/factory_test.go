// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[llmrouter.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, llmrouter.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheStreamingGenerationBudget(t *testing.T) {
	step := llmrouter.NewGenerateTextStep(llmrouter.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: testConnection.Name, Annotations: llmAnnotations(),
		Connection: factoryConnection(t), MapToOperationInput: mapToUserRequest, Generated: sdkgo.GoTo(generatedTarget{}),
	})
	options := step.GetStepOptions()
	require.Equal(t, dex.StepDurabilitySync, options.ExecuteDurability)
	require.Equal(t, 900*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, 60*time.Second, options.HeartbeatTimeout)
	require.Equal(t, &dex.RetryPolicy{
		InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute,
		MaximumAttempts: 4, TotalDuration: 30 * time.Minute,
	}, options.ExecuteRetry)
}

// TestStepDefaultsEqualEveryRoutedProvidersBudget keeps llm's Step options equal to each pinned provider's generateText defaults.
func TestStepDefaultsEqualEveryRoutedProvidersBudget(t *testing.T) {
	for connectorID, definition := range map[string]sdkgo.QueryDefinition{
		openai.ConnectorID: openai.GenerateTextDefinition, claude.ConnectorID: claude.GenerateTextDefinition,
		gemini.ConnectorID: gemini.GenerateTextDefinition,
	} {
		require.Equal(t, definition.StepDefaults, llmrouter.GenerateTextDefinition.StepDefaults, connectorID)
		require.Equal(t, llm.TextGenerationOperationID, definition.Operation.OperationID, connectorID)
	}
	require.Equal(t, llm.TextGenerationOperationID, llmrouter.GenerateTextDefinition.Operation.OperationID)
	require.Equal(t, "llm", llmrouter.GenerateTextDefinition.Operation.ConnectorID)
}

func TestGenerateTextFactoryFailsClosed(t *testing.T) {
	connection := factoryConnection(t)
	require.Panics(t, func() {
		llmrouter.NewGenerateTextStep(llmrouter.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: llmAnnotations(), Connection: connection, MapToOperationInput: mapToUserRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		llmrouter.NewGenerateTextStep(llmrouter.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: llmAnnotations(),
			Connection: connection, MapToOperationInput: mapToUserRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		llmrouter.NewGenerateTextStep(llmrouter.GenerateTextStepConfig[string]{Connection: llmrouter.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "llmrouter.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) llmrouter.Connection {
	t.Helper()
	client, err := llmrouter.New(llmrouter.Config{Model: "anthropic/claude-sonnet-5"}, staticCredentials(allTestAPIKeys()))
	require.NoError(t, err)
	connection, err := llmrouter.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func llmAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "llm", GroupLabel: "LLM", Explanation: "Generate an answer with the selected model."}
}

func mapToUserRequest(text string) llmrouter.GenerateTextRequest {
	return userRequest("", text)
}
