// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package summarizetext

import (
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGenerateTextRequestSendsTheStepPick(t *testing.T) {
	request := NewFlow(gemini.Connection{}, SummaryModelConfiguration{}).MapToGenerateTextRequest(SummaryRequest{Text: "The connector shipped."})
	require.Empty(t, request.Model, "the connection's model applies without a pick")
	require.NotEmpty(t, request.Instructions)
	require.Equal(t, []llm.Message{{Role: llm.MessageRoleUser, Text: "The connector shipped."}}, request.Messages)
	require.Equal(t, 4096, request.MaxOutputTokens)
	require.Empty(t, request.ReasoningEffort, "every Gemini model keeps its default thinking level")
	require.Nil(t, request.Temperature, "Google recommends the default temperature for Gemini 3 models")

	picked := NewFlow(gemini.Connection{}, SummaryModelConfiguration{Model: " gemini-3.8-flash "})
	require.Equal(t, "gemini-3.8-flash", picked.MapToGenerateTextRequest(SummaryRequest{Text: "x"}).Model)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "GeminiSummarizeText", dex.GetFinalFlowType(NewFlow(gemini.Connection{}, SummaryModelConfiguration{})))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "RecordSummaryOutcome", dex.GetFinalStepType[gemini.GenerateTextResult](recordSummaryOutcome{}))
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "gemini", ConnectionName: ConnectionName, OperationID: "generateText",
		FlowType: "GeminiSummarizeText", StepType: "SummarizeText",
	}, SummaryModelConfigurationRef())
	// Dex Web Start Flow invokes the start Step's WaitFor, so it must be a real, immediate WaitFor.
	wait, err := recordSummaryRequest{}.WaitFor(nil, SummaryRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[gemini.Credentials]{}
	client, err := gemini.New(gemini.Config{}, credentials)
	require.NoError(t, err)
	connection, err := gemini.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, SummaryModelConfiguration{})})
	require.NoError(t, err)

	otherConnection, err := gemini.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, SummaryModelConfiguration{})}) },
		"the static ConnectionName must match the runtime connection")
}
