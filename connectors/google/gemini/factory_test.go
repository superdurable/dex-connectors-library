// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[gemini.GenerateContentResult]
}

func (generatedTarget) Execute(dex.Context, gemini.GenerateContentResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func geminiAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "gemini", GroupLabel: "Gemini", Explanation: "Generate content with Gemini."}
}

func factoryConnection(t *testing.T, name string) gemini.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: name}
	client, err := gemini.New(gemini.Config{}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	})
	require.NoError(t, err)
	connection, err := gemini.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func mapPrompt(prompt string) gemini.GenerateContentRequest {
	return gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt(prompt)}
}

func TestGenerateContentDefinitionRequiresOnlyTheHappyPath(t *testing.T) {
	definition := gemini.GenerateContentDefinition
	require.NoError(t, definition.Validate())
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "gemini", OperationID: "generateContent"}, definition.Operation)
	optional := map[sdkgo.BranchID]bool{}
	for _, branch := range definition.Branches {
		optional[branch.ID] = branch.Optional
	}
	require.Equal(t, map[sdkgo.BranchID]bool{
		"generated": false, "truncated": true, "blocked": true, "providerRejected": true, "invalidResponse": true, "defect": true,
	}, optional)
}

func TestGenerateContentFactoryAppliesLongGenerationDefaults(t *testing.T) {
	result := dex.DefineAttribute[gemini.GenerateContentResult]("gemini-factory-result")
	step := gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
		StepType: "GenerateArticle", ConnectionName: "gemini-factory", Annotations: geminiAnnotations(),
		Connection: factoryConnection(t, "gemini-factory"), MapToOperationInput: mapPrompt,
		Generated: sdkgo.GoTo(generatedTarget{}), Truncated: sdkgo.GoTo(generatedTarget{}), Blocked: sdkgo.GoTo(generatedTarget{}),
		ProviderRejected: sdkgo.GoTo(generatedTarget{}), InvalidResponse: sdkgo.GoTo(generatedTarget{}), Defect: sdkgo.GoTo(generatedTarget{}),
		ResultAttribute: &result,
	})
	require.Equal(t, "GenerateArticle", step.GetStepType())
	options := step.GetStepOptions()
	require.Equal(t, 300*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, 300*time.Second, options.HeartbeatTimeout, "a silent non-streaming generation must outlive the one-minute heartbeat default")
	require.Equal(t, dex.StepDurabilitySync, options.ExecuteDurability)
	require.Equal(t, &dex.RetryPolicy{
		InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute,
		MaximumAttempts: 5, TotalDuration: 10 * time.Minute,
	}, options.ExecuteRetry)
}

func TestGenerateContentFactoryMergesApplicationOverrides(t *testing.T) {
	step := gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
		StepType: "InterpretRequest", Annotations: geminiAnnotations(), Connection: factoryConnection(t, "gemini-factory"),
		MapToOperationInput: mapPrompt, Generated: sdkgo.GoTo(generatedTarget{}),
		StepOptionsOverride: &dex.StepOptions{ExecuteDurability: dex.StepDurabilityAsync, ExecuteMethodTimeout: 30 * time.Second},
	})
	options := step.GetStepOptions()
	require.Equal(t, dex.StepDurabilityAsync, options.ExecuteDurability)
	require.Equal(t, 30*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, 300*time.Second, options.HeartbeatTimeout)
	require.Panics(t, func() {
		gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
			StepType: "InterpretRequest", Annotations: geminiAnnotations(), Connection: factoryConnection(t, "gemini-factory"),
			MapToOperationInput: mapPrompt, Generated: sdkgo.GoTo(generatedTarget{}),
			StepOptionsOverride: &dex.StepOptions{WaitForMethodTimeout: time.Second},
		})
	}, "execute-only Connector Steps reject WaitFor options")
}

func TestGenerateContentFactoryFailsClosed(t *testing.T) {
	connection := factoryConnection(t, "gemini-factory")
	require.Panics(t, func() {
		gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
			StepType: "GenerateArticle", Annotations: geminiAnnotations(), Connection: connection, MapToOperationInput: mapPrompt,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
			StepType: "GenerateArticle", ConnectionName: "another-connection", Annotations: geminiAnnotations(),
			Connection: connection, MapToOperationInput: mapPrompt, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{Connection: gemini.Connection{}})
	}, "a zero Connection is rejected")
	require.Equal(t, "gemini.Connection{[REDACTED]}", connection.String())
}
