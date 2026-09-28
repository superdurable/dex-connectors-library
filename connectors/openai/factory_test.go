// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createTarget struct {
	dex.StepDefaultsNoWaitFor[openai.CreateResponseResult]
}

func (createTarget) Execute(dex.Context, openai.CreateResponseResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type retrieveTarget struct {
	dex.StepDefaultsNoWaitFor[openai.RetrieveResponseResult]
}

func (retrieveTarget) Execute(dex.Context, openai.RetrieveResponseResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestOperationSpecificFactoriesUseTypedConnectionAndResources(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "openai", Name: "factory-test"}
	client, err := openai.New(openai.Config{}, sdkgo.StaticCredentialProvider[openai.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("test-key")},
	})
	require.NoError(t, err)
	connection, err := openai.NewConnection(client, reference)
	require.NoError(t, err)
	result := dex.DefineAttribute[sdkgo.MutationResult[openai.Response]]("openai-factory-result")
	progress := dex.DefineStream[sdkgo.ProgressUpdate]("openai-factory-progress", 100)
	text := dex.DefineStream[string]("openai-factory-text", 100)

	create := openai.NewCreateResponseStep(openai.CreateResponseStepConfig[string]{
		StepType: "CreateResponse", Annotations: openAIAnnotations(), Connection: connection,
		MapToOperationInput: func(string) openai.CreateRequest { return openai.CreateRequest{Model: "gpt-test"} },
		Completed:           sdkgo.GoTo(createTarget{}), Failed: sdkgo.GoTo(createTarget{}),
		ProviderRejected: sdkgo.GoTo(createTarget{}),
		Uncertain:        sdkgo.GoTo(createTarget{}), Defect: sdkgo.GoTo(createTarget{}),
		ResultAttribute: &result, ProgressStream: &progress, TextStream: &text,
	})
	require.Equal(t, "CreateResponse", create.GetStepType())
	createOptions := create.GetStepOptions()
	require.Equal(t, dex.StepDurabilitySync, createOptions.ExecuteDurability)
	require.Equal(t, 150*time.Second, createOptions.ExecuteMethodTimeout)

	retrieve := openai.NewRetrieveResponseStep(openai.RetrieveResponseStepConfig[string]{
		StepType: "RetrieveResponse", Annotations: openAIAnnotations(), Connection: connection,
		MapToOperationInput: func(string) openai.RetrieveRequest { return openai.RetrieveRequest{ResponseID: "resp_1"} },
		Found:               sdkgo.GoTo(retrieveTarget{}), NotFound: sdkgo.GoTo(retrieveTarget{}),
		ProviderRejected: sdkgo.GoTo(retrieveTarget{}), InvalidResponse: sdkgo.GoTo(retrieveTarget{}),
		Defect: sdkgo.GoTo(retrieveTarget{}),
	})
	require.Equal(t, "RetrieveResponse", retrieve.GetStepType())
	retrieveOptions := retrieve.GetStepOptions()
	require.Equal(t, dex.StepDurabilityAsync, retrieveOptions.ExecuteDurability)
	require.Equal(t, 30*time.Second, retrieveOptions.ExecuteMethodTimeout)
}

func TestOpenAIDefinitionsOwnTheirBranchIdentities(t *testing.T) {
	createBranches := map[sdkgo.BranchID]bool{}
	for _, branch := range openai.CreateResponseDefinition.Branches {
		createBranches[branch.ID] = branch.Optional
	}
	require.Equal(t, map[sdkgo.BranchID]bool{
		"completed": false, "failed": true, "providerRejected": true, "uncertain": true, "defect": true,
	}, createBranches)

	retrieveBranches := map[sdkgo.BranchID]bool{}
	for _, branch := range openai.RetrieveResponseDefinition.Branches {
		retrieveBranches[branch.ID] = branch.Optional
	}
	require.Equal(t, map[sdkgo.BranchID]bool{
		"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true,
	}, retrieveBranches)
}

func TestTypedConnectionCannotSerializeAndRequiredBranchFailsClosed(t *testing.T) {
	encoded, err := json.Marshal(openai.Connection{})
	require.ErrorContains(t, err, "cannot be serialized")
	require.Nil(t, encoded)
	require.NotContains(t, fmt.Sprintf("%#v", openai.Connection{}), "client")
	require.Panics(t, func() {
		openai.NewCreateResponseStep(openai.CreateResponseStepConfig[string]{Connection: openai.Connection{}})
	})
}

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[openai.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, openai.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheStreamingGenerationBudget(t *testing.T) {
	step := openai.NewGenerateTextStep(openai.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: generateTextConnection.Name, Annotations: openAIAnnotations(),
		Connection: generateTextFactoryConnection(t), MapToOperationInput: generateTextUserRequest, Generated: sdkgo.GoTo(generatedTarget{}),
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

func TestGenerateTextFactoryFailsClosed(t *testing.T) {
	connection := generateTextFactoryConnection(t)
	require.Panics(t, func() {
		openai.NewGenerateTextStep(openai.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: openAIAnnotations(), Connection: connection, MapToOperationInput: generateTextUserRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		openai.NewGenerateTextStep(openai.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: openAIAnnotations(),
			Connection: connection, MapToOperationInput: generateTextUserRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		openai.NewGenerateTextStep(openai.GenerateTextStepConfig[string]{Connection: openai.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "openai.Connection{[REDACTED]}", rendering)
	}
}

func generateTextFactoryConnection(t *testing.T) openai.Connection {
	t.Helper()
	client, err := openai.New(openai.Config{}, generateTextCredentials())
	require.NoError(t, err)
	connection, err := openai.NewConnection(client, generateTextConnection)
	require.NoError(t, err)
	return connection
}

func openAIAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "openai", GroupLabel: "OpenAI", Explanation: "Invoke the OpenAI sdkgo."}
}
