// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package generatesummary

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMapToGenerateContentRequestAsksForStructuredJSON(t *testing.T) {
	request := NewFlow(gemini.Connection{}, "").MapToGenerateContentRequest(SummaryRequest{Title: "Launch", Text: "The connector shipped."})
	require.Equal(t, SummaryModel, request.Model)
	require.NotEmpty(t, request.SystemInstruction)
	require.Len(t, request.Contents, 1)
	require.Equal(t, "user", request.Contents[0].Role)
	require.Equal(t, "Title: Launch\n\nThe connector shipped.", request.Contents[0].Parts[0].Text)
	require.Equal(t, SummarySchema(), request.ResponseJSONSchema)
	require.Empty(t, request.ResponseMIMEType, "the connector defaults a JSON Schema request to application/json")
	require.Equal(t, 4096, request.MaxOutputTokens)
}

// TestSummaryModelIsOpenToNewProjects keeps the example off Gemini 2.x models, which a new Google AI Studio project cannot call.
func TestSummaryModelIsOpenToNewProjects(t *testing.T) {
	require.Equal(t, "gemini-3.5-flash-lite", SummaryModel)
	for _, restricted := range []string{"gemini-2.0-", "gemini-2.5-"} {
		require.NotContains(t, SummaryModel, restricted)
	}
	request := NewFlow(gemini.Connection{}, "").MapToGenerateContentRequest(SummaryRequest{Title: "Launch", Text: "The connector shipped."})
	// Google recommends Gemini 3's default temperature, and thinkingBudget is legacy for them, so the example sends neither.
	require.Nil(t, request.Temperature)
	require.Nil(t, request.ThinkingBudget)
}

func TestNewFlowUsesTheConfiguredModel(t *testing.T) {
	for _, testCase := range []struct{ configured, want string }{
		{"", SummaryModel}, {"  ", SummaryModel}, {"gemini-3.8-flash", "gemini-3.8-flash"}, {" gemini-3.8-flash ", "gemini-3.8-flash"},
	} {
		flow := NewFlow(gemini.Connection{}, testCase.configured)
		require.Equal(t, testCase.want, flow.Model(), "configured %q", testCase.configured)
		require.Equal(t, testCase.want, flow.MapToGenerateContentRequest(SummaryRequest{Text: "x"}).Model, "configured %q", testCase.configured)
	}
}

func TestSummarySchemaDescribesTheDecodedSummary(t *testing.T) {
	encoded, err := json.Marshal(SummarySchema())
	require.NoError(t, err)
	var schema struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	require.NoError(t, json.Unmarshal(encoded, &schema))
	require.Equal(t, "object", schema.Type)
	require.ElementsMatch(t, []string{"headline", "summary", "keyPoints"}, schema.Required)
	summaryJSON, err := json.Marshal(Summary{Headline: "h", Summary: "s", KeyPoints: []string{"k"}})
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(summaryJSON, &fields))
	for field := range fields {
		require.Contains(t, schema.Properties, field)
	}
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "GeminiGenerateSummary", dex.GetFinalFlowType(NewFlow(gemini.Connection{}, "")))
	require.Equal(t, "RecordSummaryRequest", dex.GetFinalStepType[SummaryRequest](recordSummaryRequest{}))
	require.Equal(t, "SummaryGenerated", dex.GetFinalStepType[gemini.GenerateContentResult](summaryGenerated{}))
	require.Equal(t, "SummaryNotGenerated", dex.GetFinalStepType[gemini.GenerateContentResult](summaryNotGenerated{}))
	// Dex Web Start Flow invokes the start Step's WaitFor, so it must be a real, immediate WaitFor.
	wait, err := recordSummaryRequest{}.WaitFor(nil, SummaryRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersWithTheGeminiConnection(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	client, err := gemini.New(gemini.Config{}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("AIza-unit-test")},
	})
	require.NoError(t, err)
	connection, err := gemini.NewConnection(client, reference)
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, "")})
	require.NoError(t, err)

	otherReference := sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"}
	otherConnection, err := gemini.NewConnection(client, otherReference)
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, "")}) },
		"the static ConnectionName must match the runtime connection")
}
