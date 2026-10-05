// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	escalateblocked "github.com/superdurable/dex-connectors-library/connectors/clickup/examples/escalate-blocked-task/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly against the example's types.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 2)

	var binding struct {
		ConnectorID    string                                `json:"connectorId"`
		ConnectionName string                                `json:"connectionName"`
		TriggerName    string                                `json:"triggerName"`
		BindingName    string                                `json:"bindingName"`
		Configuration  clickup.TaskEventTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[0], &binding)
	require.Equal(t, clickup.ConnectorID, binding.ConnectorID)
	require.Equal(t, escalateblocked.ConnectionName, binding.ConnectionName)
	require.Equal(t, clickup.TaskEventTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, escalateblocked.BlockedStatusTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())
	require.Equal(t, blockedStatusBinding, binding.Configuration)

	var outcome escalateblocked.EscalationOutcome
	requireStrictJSON(t, samples[1], &outcome)
	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	require.Contains(t, readme, escalateblocked.FlowIDPrefix+"<task ID>-<history item ID>")
	require.Contains(t, readme, webhookPath)
	require.Contains(t, readme, localAPIBaseURLEnvironmentVariable)
	for _, variable := range []string{"CLICKUP_WORKSPACE_ID", "CLICKUP_ESCALATION_LIST_ID", "CLICKUP_ESCALATION_MANAGER_EMAIL", "CLICKUP_BLOCKED_STATUS"} {
		require.Contains(t, readme, variable)
		require.Contains(t, readFile(t, "main.go"), variable)
	}
}

// TestConnectorREADMESamplesAreTheExampleCode keeps the connector README's record and Go snippet runnable.
func TestConnectorREADMESamplesAreTheExampleCode(t *testing.T) {
	readme := readFile(t, filepath.Join("..", "..", "README.md"))
	snippets := fencedBlocks(readme, "go")
	require.Len(t, snippets, 1)
	require.Contains(t, readFile(t, "main.go"), snippets[0])

	records := fencedBlocks(readme, "json")
	require.Len(t, records, 2)
	var record projectconfig.ConnectionConfiguration
	requireStrictJSON(t, records[0], &record)
	require.Equal(t, clickup.ConnectorID, record.ConnectorID)
	require.Equal(t, escalateblocked.ConnectionName, record.ConnectionName)
	require.Equal(t, "github.com/superdurable/dex-connectors-library/connectors/clickup", record.ModulePath)
	require.Equal(t, "clickup", record.Provider)
	var configuration clickup.Config
	requireStrictJSON(t, string(record.Configuration), &configuration)
	require.NoError(t, configuration.Validate())

	var webhook struct {
		Endpoint string   `json:"endpoint"`
		Events   []string `json:"events"`
		ListID   int64    `json:"list_id"`
	}
	requireStrictJSON(t, records[1], &webhook)
	require.True(t, strings.HasSuffix(webhook.Endpoint, webhookPath))
	require.NoError(t, clickup.TaskEventTriggerConfiguration{Events: webhook.Events}.Validate())
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}

func fencedBlocks(markdown string, language string) []string {
	var blocks []string
	for _, match := range regexp.MustCompile("(?s)```"+language+"\n(.*?)```").FindAllStringSubmatch(markdown, -1) {
		blocks = append(blocks, strings.TrimSuffix(match[1], "\n"))
	}
	return blocks
}

func requireStrictJSON(t *testing.T, sample string, destination any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader([]byte(sample)))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(destination), sample)
}
