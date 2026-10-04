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
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	answerduplicate "github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly against the example's types.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 3)

	var useConfiguration struct {
		ConnectorID    string                             `json:"connectorId"`
		ConnectionName string                             `json:"connectionName"`
		OperationID    string                             `json:"operationId"`
		FlowType       string                             `json:"flowType"`
		StepType       string                             `json:"stepType"`
		Configuration  answerduplicate.ReplyConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[0], &useConfiguration)
	reference := answerduplicate.ReplyConfigurationRef()
	require.Equal(t, []string{reference.ConnectorID, reference.ConnectionName, reference.OperationID, reference.FlowType, reference.StepType},
		[]string{useConfiguration.ConnectorID, useConfiguration.ConnectionName, useConfiguration.OperationID, useConfiguration.FlowType, useConfiguration.StepType})
	require.Regexp(t, `^[0-9]+$`, useConfiguration.Configuration.AdminID)

	var binding struct {
		ConnectorID    string                                         `json:"connectorId"`
		ConnectionName string                                         `json:"connectionName"`
		TriggerName    string                                         `json:"triggerName"`
		BindingName    string                                         `json:"bindingName"`
		Configuration  intercom.ConversationEventTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[1], &binding)
	require.Equal(t, intercom.ConnectorID, binding.ConnectorID)
	require.Equal(t, answerduplicate.ConnectionName, binding.ConnectionName)
	require.Equal(t, intercom.ConversationEventTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, answerduplicate.InboundTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())
	require.Equal(t, []string{intercom.TopicConversationUserCreated}, binding.Configuration.Topics)

	var outcome answerduplicate.DuplicateConversationOutcome
	requireStrictJSON(t, samples[2], &outcome)
	require.Equal(t, answerduplicate.OutcomeAnsweredAndClosed, outcome.Action)
	require.Equal(t, intercom.ConversationStateClosed, outcome.ConversationState)
	require.Contains(t, readme, answerduplicate.FlowIDPrefix+"<conversation ID>")
	require.Contains(t, readme, webhookPath)
	require.Contains(t, readme, localAPIBaseURLEnvironmentVariable)
}

// TestConnectorREADMESamplesAreTheExampleCode keeps the connector README's record and Go snippet runnable.
func TestConnectorREADMESamplesAreTheExampleCode(t *testing.T) {
	readme := readFile(t, filepath.Join("..", "..", "README.md"))
	snippets := fencedBlocks(readme, "go")
	require.Len(t, snippets, 1)
	require.Contains(t, readFile(t, "main.go"), snippets[0])

	records := fencedBlocks(readme, "json")
	require.Len(t, records, 1)
	var record projectconfig.ConnectionConfiguration
	requireStrictJSON(t, records[0], &record)
	require.Equal(t, intercom.ConnectorID, record.ConnectorID)
	require.Equal(t, answerduplicate.ConnectionName, record.ConnectionName)
	require.Equal(t, "github.com/superdurable/dex-connectors-library/connectors/intercom", record.ModulePath)
	require.Equal(t, "intercom", record.Provider)
	var configuration intercom.Config
	requireStrictJSON(t, string(record.Configuration), &configuration)
	require.Equal(t, intercom.RegionUs, configuration.Region)
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
