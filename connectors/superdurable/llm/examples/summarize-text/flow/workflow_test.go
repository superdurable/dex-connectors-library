// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGenerateTextRequestSendsAPortableRequest(t *testing.T) {
	request := NewFlow(llm.Connection{}, SummaryModelConfiguration{}).MapToGenerateTextRequest(SummaryRequest{Text: "The connector shipped."})
	require.Empty(t, request.Model, "the connection default applies without a pick")
	require.NotEmpty(t, request.Instructions)
	require.Equal(t, []textgen.Message{{Role: textgen.MessageRoleUser, Text: "The connector shipped."}}, request.Messages)
	require.Nil(t, request.Temperature, "current Claude and Kimi models accept no other temperature")
	require.Empty(t, request.ReasoningEffort, "providers and models offer different efforts")
	require.Zero(t, request.MaxOutputTokens, "a small limit truncates reasoning models, whose thinking counts against it")
	require.Nil(t, request.StructuredOutput)

	picked := NewFlow(llm.Connection{}, SummaryModelConfiguration{Model: " claude-haiku-4-5 "})
	require.Equal(t, "claude-haiku-4-5", picked.MapToGenerateTextRequest(SummaryRequest{Text: "x"}).Model)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "LLMSummarizeText", dex.GetFinalFlowType(NewFlow(llm.Connection{}, SummaryModelConfiguration{})))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "RecordSummaryOutcome", dex.GetFinalStepType[llm.GenerateTextResult](recordSummaryOutcome{}))
	require.Equal(t, "RecordSummaryFailure", dex.GetFinalStepType[llm.GenerateTextResult](recordSummaryFailure{}))
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "llm", ConnectionName: "llm", OperationID: "generateText",
		FlowType: "LLMSummarizeText", StepType: "SummarizeText",
	}, SummaryModelConfigurationRef())
	// Dex Web Start Flow invokes the start Step's WaitFor, so it must be a real, immediate WaitFor.
	wait, err := recordSummaryRequest{}.WaitFor(nil, SummaryRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[llm.Credentials]{}
	client, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic}, credentials)
	require.NoError(t, err)
	connection, err := llm.NewConnection(client, sdkgo.ConnectionRef{Provider: "llm", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, SummaryModelConfiguration{})})
	require.NoError(t, err)

	otherConnection, err := llm.NewConnection(client, sdkgo.ConnectionRef{Provider: "llm", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, SummaryModelConfiguration{})}) },
		"the static ConnectionName must match the runtime connection")
}
