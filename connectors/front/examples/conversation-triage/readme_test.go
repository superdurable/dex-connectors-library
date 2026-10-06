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
	"github.com/superdurable/dex-connectors-library/connectors/front"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/front/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly against the example's types.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 3)

	var useConfiguration struct {
		ConnectorID    string                                  `json:"connectorId"`
		ConnectionName string                                  `json:"connectionName"`
		OperationID    string                                  `json:"operationId"`
		FlowType       string                                  `json:"flowType"`
		StepType       string                                  `json:"stepType"`
		Configuration  conversationtriage.RoutingConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[0], &useConfiguration)
	reference := conversationtriage.RoutingConfigurationRef()
	require.Equal(t, []string{reference.ConnectorID, reference.ConnectionName, reference.OperationID, reference.FlowType, reference.StepType},
		[]string{useConfiguration.ConnectorID, useConfiguration.ConnectionName, useConfiguration.OperationID, useConfiguration.FlowType, useConfiguration.StepType})
	require.Regexp(t, `^tag_[a-z0-9]+$`, useConfiguration.Configuration.TagID)
	require.Regexp(t, `^tea_[a-z0-9]+$`, useConfiguration.Configuration.AssigneeID)

	var request conversationtriage.TriageRequest
	requireStrictJSON(t, samples[1], &request)
	require.Regexp(t, `^cnv_[a-z0-9]+$`, request.ConversationID)

	var triage conversationtriage.Triage
	requireStrictJSON(t, samples[2], &triage)
	require.Equal(t, "completed", triage.Stage)
	require.Equal(t, front.ConversationStatusAssigned, triage.Status)
	require.Contains(t, readme, localAPIBaseURLEnvironmentVariable)
	require.Contains(t, readme, conversationtriage.TriageCommentPrefix)
	require.Contains(t, readme, conversationtriage.FlowType)
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
	require.Equal(t, front.ConnectorID, record.ConnectorID)
	require.Equal(t, conversationtriage.ConnectionName, record.ConnectionName)
	require.Equal(t, "github.com/superdurable/dex-connectors-library/connectors/front", record.ModulePath)
	require.Equal(t, "front", record.Provider)
	var configuration front.Config
	requireStrictJSON(t, string(record.Configuration), &configuration)
	require.Zero(t, configuration.MaxResponseBytes, "a blank limit uses the manifest default")
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
