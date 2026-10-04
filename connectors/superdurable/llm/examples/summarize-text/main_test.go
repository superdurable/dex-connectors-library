// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the project configuration that Dex Web saves.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(summaryProject(t, `{"model":"gemini-3.8-flash"}`).Configuration)
	require.NoError(t, err)
	require.Equal(t, "gemini-3.8-flash", picked.Model)

	inherited, err := loadSummaryModelConfiguration(summaryProject(t, `{"model":""}`).Configuration)
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection default")

	neverConfigured, err := loadSummaryModelConfiguration(summaryProject(t, "").Configuration)
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection default")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(summaryProject(t, `{"model":"gpt-6-luna","temperature":1}`).Configuration)
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

// TestProjectConnectionNeedsAProviderAndTakesAnOptionalModel opens the connection the Worker uses from project storage.
func TestProjectConnectionNeedsAProviderAndTakesAnOptionalModel(t *testing.T) {
	for _, configuration := range []map[string]any{
		{"provider": "anthropic", "model": "claude-opus-5-5"},
		{"provider": "openai"},
		{"provider": "qwen", "region": "hong-kong"},
	} {
		project := projectWithConnection(t, configuration, map[string]any{"api_key": "sk-SENTINEL-llm-project-connection"})
		connection, err := llm.NewProjectConnection(project, summarizetext.ConnectionName)
		require.NoError(t, err, "%v", configuration)
		require.Equal(t, "llm.Connection{[REDACTED]}", connection.String())
	}

	project := projectWithConnection(t, map[string]any{"model": "gpt-6-sol"}, map[string]any{"api_key": "sk-SENTINEL"})
	_, err := llm.NewProjectConnection(project, summarizetext.ConnectionName)
	require.ErrorContains(t, err, "configuration provider is required")

	project = projectWithConnection(t, map[string]any{"provider": "openai", "model": "anthropic/claude-sonnet-5", "maxTokens": 10},
		map[string]any{"api_key": "sk-SENTINEL"})
	_, err = llm.NewProjectConnection(project, summarizetext.ConnectionName)
	require.Error(t, err, "settings decode strictly, so a field the manifest does not declare stops the Worker")

	project = projectWithConnection(t, map[string]any{"provider": "openai", "region": "eu"}, map[string]any{"api_key": "sk-SENTINEL"})
	_, err = llm.NewProjectConnection(project, summarizetext.ConnectionName)
	require.ErrorContains(t, err, "provider openai does not serve region eu")

	_, err = llm.NewProjectConnection(project, "another-connection")
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound, "a connection that dex-app.yaml does not declare is missing")
}

// summaryProject saves an openai llm connection and, when stepConfiguration is set, the SummarizeText Step's pick.
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
	return testsupport.NewLoadedProject(t, llm.ConnectorID, []testsupport.ProjectConnection{{
		Name: summarizetext.ConnectionName, Configuration: map[string]any{"provider": "openai"},
		Credentials: map[string]any{"api_key": "sk-SENTINEL-llm-step-pick"},
	}}, operations)
}

func projectWithConnection(t *testing.T, configuration map[string]any, credentials map[string]any) *projectconfig.LoadedProject {
	t.Helper()
	return testsupport.NewLoadedProject(t, llm.ConnectorID, []testsupport.ProjectConnection{{
		Name: summarizetext.ConnectionName, Configuration: configuration, Credentials: credentials,
	}}, nil)
}
