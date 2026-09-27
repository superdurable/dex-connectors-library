// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGenerateTextRequestSendsTheStepPick(t *testing.T) {
	request := NewFlow(deepseek.Connection{}, SummaryModelConfiguration{}).MapToGenerateTextRequest(SummaryRequest{Text: "The connector shipped."})
	require.Empty(t, request.Model, "the connection's model applies without a pick")
	require.NotEmpty(t, request.Instructions)
	require.Equal(t, []llm.Message{{Role: llm.MessageRoleUser, Text: "The connector shipped."}}, request.Messages)
	require.Equal(t, 1024, request.MaxOutputTokens)
	require.Equal(t, llm.ReasoningEffortNone, request.ReasoningEffort, "a short summary turns thinking mode off")
	require.Nil(t, request.Temperature, "the connector rejects a temperature, which thinking mode ignores")

	picked := NewFlow(deepseek.Connection{}, SummaryModelConfiguration{Model: " deepseek-v4-pro "})
	require.Equal(t, "deepseek-v4-pro", picked.MapToGenerateTextRequest(SummaryRequest{Text: "x"}).Model)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "DeepSeekSummarizeText", dex.GetFinalFlowType(NewFlow(deepseek.Connection{}, SummaryModelConfiguration{})))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "RecordSummaryOutcome", dex.GetFinalStepType[deepseek.GenerateTextResult](recordSummaryOutcome{}))
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "deepseek", ConnectionName: ConnectionName, OperationID: "generateText",
		FlowType: "DeepSeekSummarizeText", StepType: "SummarizeText",
	}, SummaryModelConfigurationRef())
	// Dex Web Start Flow invokes the start Step's WaitFor, so it must be a real, immediate WaitFor.
	wait, err := recordSummaryRequest{}.WaitFor(nil, SummaryRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[deepseek.Credentials]{}
	client, err := deepseek.New(deepseek.Config{}, credentials)
	require.NoError(t, err)
	connection, err := deepseek.NewConnection(client, sdkgo.ConnectionRef{Provider: "deepseek", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, SummaryModelConfiguration{})})
	require.NoError(t, err)

	otherConnection, err := deepseek.NewConnection(client, sdkgo.ConnectionRef{Provider: "deepseek", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, SummaryModelConfiguration{})}) },
		"the static ConnectionName must match the runtime connection")
}
