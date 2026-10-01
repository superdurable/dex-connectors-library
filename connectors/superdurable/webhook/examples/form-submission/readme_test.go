// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	formsubmission "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/examples/form-submission/flow"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly and checks the curl body.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 2)
	var binding struct {
		ConnectorID    string                                      `json:"connectorId"`
		ConnectionName string                                      `json:"connectionName"`
		TriggerName    string                                      `json:"triggerName"`
		BindingName    string                                      `json:"bindingName"`
		Configuration  webhook.RequestReceivedTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[0], &binding)
	require.Equal(t, webhook.ConnectorID, binding.ConnectorID)
	require.Equal(t, formsubmission.ConnectionName, binding.ConnectionName)
	require.Equal(t, webhook.RequestReceivedTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, formsubmission.SubmissionTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())
	require.Contains(t, readFile(t, filepath.Join("..", "..", "README.md")), samples[0], "both READMEs show the same binding")
	var outcome formsubmission.ForwardingOutcome
	requireStrictJSON(t, samples[1], &outcome)
	require.Equal(t, webhook.SendEventBranchDelivered, outcome.Branch)

	curlBody := regexp.MustCompile(`(?m)^body='(.+)'$`).FindStringSubmatch(readme)
	require.Len(t, curlBody, 2)
	var submission struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
	}
	require.NoError(t, json.Unmarshal([]byte(curlBody[1]), &submission))
	require.Equal(t, binding.Configuration.MatchValues, []string{submission.EventType})
	require.Contains(t, readme, formsubmission.FlowIDPrefix+submission.EventID)
	require.Contains(t, readme, submissionPath)
}

// TestConnectorREADMESnippetIsTheExampleCode keeps the connector README's Go snippet runnable.
func TestConnectorREADMESnippetIsTheExampleCode(t *testing.T) {
	snippets := fencedBlocks(readFile(t, filepath.Join("..", "..", "README.md")), "go")
	require.Len(t, snippets, 1)
	require.Contains(t, readFile(t, "main.go"), snippets[0])
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(contents)
}

func fencedBlocks(markdown string, language string) []string {
	var blocks []string
	var lines []string
	isInBlock := false
	for _, line := range strings.Split(markdown, "\n") {
		switch {
		case !isInBlock && line == "```"+language:
			isInBlock, lines = true, nil
		case isInBlock && line == "```":
			isInBlock = false
			blocks = append(blocks, strings.Join(lines, "\n"))
		case isInBlock:
			lines = append(lines, line)
		}
	}
	return blocks
}

func requireStrictJSON(t *testing.T, sample string, value any) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(sample))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(value), sample)
}
