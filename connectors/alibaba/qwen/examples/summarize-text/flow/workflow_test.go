// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGenerateTextRequestSendsTheStepPick(t *testing.T) {
	request := NewFlow(qwen.Connection{}, SummaryModelConfiguration{}).MapToGenerateTextRequest(SummaryRequest{Text: "The connector shipped."})
	require.Empty(t, request.Model, "the connection's model applies without a pick")
	require.NotEmpty(t, request.Instructions)
	require.Equal(t, []llm.Message{{Role: llm.MessageRoleUser, Text: "The connector shipped."}}, request.Messages)
	require.Equal(t, 8192, request.MaxOutputTokens)
	require.Empty(t, request.ReasoningEffort, "qwen3.7-plus documents no reasoning_effort")
	require.Nil(t, request.Temperature, "the model's default temperature applies")

	picked := NewFlow(qwen.Connection{}, SummaryModelConfiguration{Model: " qwen3.8-flash "})
	require.Equal(t, "qwen3.8-flash", picked.MapToGenerateTextRequest(SummaryRequest{Text: "x"}).Model)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "QwenSummarizeText", dex.GetFinalFlowType(NewFlow(qwen.Connection{}, SummaryModelConfiguration{})))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "RecordSummaryOutcome", dex.GetFinalStepType[qwen.GenerateTextResult](recordSummaryOutcome{}))
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "qwen", ConnectionName: ConnectionName, OperationID: "generateText",
		FlowType: "QwenSummarizeText", StepType: "SummarizeText",
	}, SummaryModelConfigurationRef())
	// Dex Web Start Flow invokes the start Step's WaitFor, so it must be a real, immediate WaitFor.
	wait, err := recordSummaryRequest{}.WaitFor(nil, SummaryRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[qwen.Credentials]{}
	client, err := qwen.New(qwen.Config{}, credentials)
	require.NoError(t, err)
	connection, err := qwen.NewConnection(client, sdkgo.ConnectionRef{Provider: "alibaba", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, SummaryModelConfiguration{})})
	require.NoError(t, err)

	otherConnection, err := qwen.NewConnection(client, sdkgo.ConnectionRef{Provider: "alibaba", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, SummaryModelConfiguration{})}) },
		"the static ConnectionName must match the runtime connection")
}
