// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"testing"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGenerateTextRequestSendsAPortableRequest(t *testing.T) {
	request := NewFlow(llmrouter.Connection{}, SummaryModelConfiguration{}).MapToGenerateTextRequest(SummaryRequest{Text: "The connector shipped."})
	require.Empty(t, request.Model, "the connection default applies without a pick")
	require.NotEmpty(t, request.Instructions)
	require.Equal(t, []llm.Message{{Role: llm.MessageRoleUser, Text: "The connector shipped."}}, request.Messages)
	require.Nil(t, request.Temperature, "current Claude models accept only the default temperature")
	require.Empty(t, request.ReasoningEffort, "some Claude and OpenAI models reject an effort they do not offer")
	require.Zero(t, request.MaxOutputTokens, "a small limit truncates reasoning models, whose thinking counts against it")
	require.Nil(t, request.StructuredOutput)

	picked := NewFlow(llmrouter.Connection{}, SummaryModelConfiguration{Model: " anthropic/claude-haiku-4-5 "})
	require.Equal(t, "anthropic/claude-haiku-4-5", picked.MapToGenerateTextRequest(SummaryRequest{Text: "x"}).Model)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "LLMSummarizeText", dex.GetFinalFlowType(NewFlow(llmrouter.Connection{}, SummaryModelConfiguration{})))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "RecordSummaryOutcome", dex.GetFinalStepType[llmrouter.GenerateTextResult](recordSummaryOutcome{}))
	require.Equal(t, "RecordSummaryFailure", dex.GetFinalStepType[llmrouter.GenerateTextResult](recordSummaryFailure{}))
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
	credentials := sdkgo.StaticCredentialProvider[llmrouter.Credentials]{}
	client, err := llmrouter.New(llmrouter.Config{Model: "anthropic"}, credentials)
	require.NoError(t, err)
	connection, err := llmrouter.NewConnection(client, sdkgo.ConnectionRef{Provider: "llm", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, SummaryModelConfiguration{})})
	require.NoError(t, err)

	otherConnection, err := llmrouter.NewConnection(client, sdkgo.ConnectionRef{Provider: "llm", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, SummaryModelConfiguration{})}) },
		"the static ConnectionName must match the runtime connection")
}
