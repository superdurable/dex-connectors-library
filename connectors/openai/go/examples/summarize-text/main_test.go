// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/openai/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the project configuration that Dex Web saves.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(summaryProject(t, `{"model":"gpt-6-luna"}`).Configuration)
	require.NoError(t, err)
	require.Equal(t, "gpt-6-luna", picked.Model)

	inherited, err := loadSummaryModelConfiguration(summaryProject(t, `{"model":""}`).Configuration)
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection's model")

	neverConfigured, err := loadSummaryModelConfiguration(summaryProject(t, "").Configuration)
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection's model")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(summaryProject(t, `{"model":"gpt-6-luna","temperature":1}`).Configuration)
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

// TestProjectConnectionLoadsASavedConnectionWithoutAModel opens the connection the Worker uses from project storage.
func TestProjectConnectionLoadsASavedConnectionWithoutAModel(t *testing.T) {
	connection, err := openai.NewProjectConnection(summaryProject(t, ""), summarizetext.ConnectionName)
	require.NoError(t, err)
	require.Equal(t, "openai.Connection{[REDACTED]}", connection.String())
}

// summaryProject saves an OpenAI connection and, when stepConfiguration is set, the SummarizeText Step's pick.
func summaryProject(t *testing.T, stepConfiguration string) *projectconfig.LoadedProject {
	t.Helper()
	var operations []projectconfig.OperationConfiguration
	if stepConfiguration != "" {
		reference := summarizetext.SummaryModelConfigurationRef()
		operations = append(operations, projectconfig.OperationConfiguration{
			ConnectorID: reference.ConnectorID, ConnectionName: reference.ConnectionName, OperationID: reference.OperationID,
			FlowType: reference.FlowType, StepType: reference.StepType, Configuration: json.RawMessage(stepConfiguration),
		})
	}
	return testsupport.NewLoadedProject(t, openai.ConnectorID, []testsupport.ProjectConnection{{
		Name: summarizetext.ConnectionName, Configuration: map[string]any{},
		Credentials: map[string]any{"api_key": "sk-proj-SENTINEL-step-pick"},
	}}, operations)
}
