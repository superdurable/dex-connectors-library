// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type createTarget struct {
	dex.StepDefaultsNoWaitFor[openai.CreateResponseStepOutput[string]]
}

func (createTarget) Execute(dex.Context, openai.CreateResponseStepOutput[string]) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type retrieveTarget struct {
	dex.StepDefaultsNoWaitFor[openai.RetrieveResponseStepOutput[string]]
}

func (retrieveTarget) Execute(dex.Context, openai.RetrieveResponseStepOutput[string]) (*dex.StepDecision, error) {
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
		StepType: "CreateResponse", Presentation: openAIPresentation(), Connection: connection,
		BuildInput: func(string) (openai.CreateRequest, error) { return openai.CreateRequest{Model: "gpt-test"}, nil },
		Completed:  sdkgo.GoTo(createTarget{}), Failed: sdkgo.GoTo(createTarget{}),
		Uncertain: sdkgo.GoTo(createTarget{}), Defect: sdkgo.GoTo(createTarget{}),
		ResultAttribute: &result, ProgressStream: &progress, TextStream: &text,
	})
	require.Equal(t, "CreateResponse", create.GetStepType())

	retrieve := openai.NewRetrieveResponseStep(openai.RetrieveResponseStepConfig[string]{
		StepType: "RetrieveResponse", Presentation: openAIPresentation(), Connection: connection,
		BuildInput: func(string) (openai.RetrieveRequest, error) { return openai.RetrieveRequest{ResponseID: "resp_1"}, nil },
		Found:      sdkgo.GoTo(retrieveTarget{}), Failed: sdkgo.GoTo(retrieveTarget{}), Defect: sdkgo.GoTo(retrieveTarget{}),
	})
	require.Equal(t, "RetrieveResponse", retrieve.GetStepType())
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

func openAIPresentation() sdkgo.StepPresentation {
	return sdkgo.StepPresentation{GroupID: "openai", GroupLabel: "OpenAI", Explanation: "Invoke the OpenAI sdkgo."}
}
