// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[llm.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, llm.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// TestGenerateTextFactoryAppliesTheQueuedStreamingBudget covers DeepSeek's 1170-second exchange and every shorter one.
func TestGenerateTextFactoryAppliesTheQueuedStreamingBudget(t *testing.T) {
	step := llm.NewGenerateTextStep(llm.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: routingTestConnection.Name, Annotations: llmAnnotations(),
		Connection: factoryConnection(t), MapToOperationInput: mapToUserRequest, Generated: sdkgo.GoTo(generatedTarget{}),
	})
	options := step.GetStepOptions()
	require.Equal(t, dex.StepDurabilitySync, options.ExecuteDurability)
	require.Equal(t, 1200*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, 60*time.Second, options.HeartbeatTimeout)
	require.Equal(t, &dex.RetryPolicy{
		InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute,
		MaximumAttempts: 4, TotalDuration: 30 * time.Minute,
	}, options.ExecuteRetry)
}

// TestGenerateTextDefinitionMatchesTheSharedContract keeps the operation ID and branch set every textgen Query requires.
func TestGenerateTextDefinitionMatchesTheSharedContract(t *testing.T) {
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "llm", OperationID: textgen.TextGenerationOperationID}, llm.GenerateTextDefinition.Operation)
	expected := textgen.TextGenerationBranchDefinitions()
	require.Len(t, llm.GenerateTextDefinition.Branches, len(expected))
	for index, branch := range llm.GenerateTextDefinition.Branches {
		require.Equal(t, expected[index].ID, branch.ID)
		require.Equal(t, expected[index].Optional, branch.Optional, branch.ID)
	}
}

func TestGenerateTextFactoryFailsClosed(t *testing.T) {
	connection := factoryConnection(t)
	require.Panics(t, func() {
		llm.NewGenerateTextStep(llm.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: routingTestConnection.Name, Annotations: llmAnnotations(),
			Connection: connection, MapToOperationInput: mapToUserRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		llm.NewGenerateTextStep(llm.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: llmAnnotations(), Connection: connection, MapToOperationInput: mapToUserRequest,
			Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a Step without its static ConnectionName is rejected")
	require.Panics(t, func() {
		llm.NewGenerateTextStep(llm.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: llmAnnotations(),
			Connection: connection, MapToOperationInput: mapToUserRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		llm.NewGenerateTextStep(llm.GenerateTextStepConfig[string]{Connection: llm.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "llm.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) llm.Connection {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic, Model: "claude-sonnet-5"}, routingCredentials())
	require.NoError(t, err)
	connection, err := llm.NewConnection(client, routingTestConnection)
	require.NoError(t, err)
	return connection
}

func llmAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "llm", GroupLabel: "LLM", Explanation: "Generate an answer with the connection's model."}
}

func mapToUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}
