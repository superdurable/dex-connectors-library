// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToCreateRequestSendsTheStepPick(t *testing.T) {
	request := NewFlow(openai.Connection{}, SummaryModelConfiguration{}).MapToCreateRequest(SummaryRequest{Text: "The connector shipped."})
	require.Empty(t, request.Model, "the connection's model applies without a pick")
	require.NotEmpty(t, request.Instructions)
	require.Equal(t, "The connector shipped.", request.Input)
	require.Nil(t, request.StructuredOutput)

	picked := NewFlow(openai.Connection{}, SummaryModelConfiguration{Model: " gpt-6-luna "})
	require.Equal(t, "gpt-6-luna", picked.MapToCreateRequest(SummaryRequest{Text: "x"}).Model)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "OpenAISummarizeText", dex.GetFinalFlowType(NewFlow(openai.Connection{}, SummaryModelConfiguration{})))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "RecordSummaryOutcome", dex.GetFinalStepType[openai.CreateResponseResult](recordSummaryOutcome{}))
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "openai", ConnectionName: ConnectionName, OperationID: "createResponse",
		FlowType: "OpenAISummarizeText", StepType: "SummarizeText",
	}, SummaryModelConfigurationRef())
	// Dex Web Start Flow invokes the start Step's WaitFor, so it must be a real, immediate WaitFor.
	wait, err := recordSummaryRequest{}.WaitFor(nil, SummaryRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[openai.Credentials]{}
	client, err := openai.New(openai.Config{}, credentials)
	require.NoError(t, err)
	connection, err := openai.NewConnection(client, sdkgo.ConnectionRef{Provider: "openai", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, SummaryModelConfiguration{})})
	require.NoError(t, err)

	otherConnection, err := openai.NewConnection(client, sdkgo.ConnectionRef{Provider: "openai", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, SummaryModelConfiguration{})}) },
		"the static ConnectionName must match the runtime connection")
}
