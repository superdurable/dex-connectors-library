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
		StepType: "CreateResponse", ConnectionName: reference.Name, Annotations: openAIAnnotations(), Connection: connection,
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
		StepType: "RetrieveResponse", ConnectionName: reference.Name, Annotations: openAIAnnotations(), Connection: connection,
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

// TestCreateResponseFactoryFailsClosed rejects a Step whose static ConnectionName differs from its Connection.
func TestCreateResponseFactoryFailsClosed(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "openai", Name: "factory-test"}
	client, err := openai.New(openai.Config{}, sdkgo.StaticCredentialProvider[openai.Credentials]{})
	require.NoError(t, err)
	connection, err := openai.NewConnection(client, reference)
	require.NoError(t, err)
	require.Panics(t, func() {
		openai.NewCreateResponseStep(openai.CreateResponseStepConfig[string]{
			StepType: "CreateResponse", Annotations: openAIAnnotations(), Connection: connection,
			MapToOperationInput: func(string) openai.CreateRequest { return openai.CreateRequest{Input: "x"} },
			Completed:           sdkgo.GoTo(createTarget{}),
		})
	}, "a Step without its static ConnectionName is rejected")
	require.Panics(t, func() {
		openai.NewCreateResponseStep(openai.CreateResponseStepConfig[string]{
			StepType: "CreateResponse", ConnectionName: "another-connection", Annotations: openAIAnnotations(), Connection: connection,
			MapToOperationInput: func(string) openai.CreateRequest { return openai.CreateRequest{Input: "x"} },
			Completed:           sdkgo.GoTo(createTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "openai.Connection{[REDACTED]}", rendering)
	}
}

func openAIAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "openai", GroupLabel: "OpenAI", Explanation: "Invoke the OpenAI sdkgo."}
}
