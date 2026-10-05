// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issueevents "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-events/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestREADMESamplesMatchTheExample decodes the binding sample strictly and delivers the curl body.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 1)
	var binding struct {
		ConnectorID    string                                        `json:"connectorId"`
		ConnectionName string                                        `json:"connectionName"`
		TriggerName    string                                        `json:"triggerName"`
		BindingName    string                                        `json:"bindingName"`
		Configuration  linear.IssueEventReceivedTriggerConfiguration `json:"configuration"`
	}
	decoder := json.NewDecoder(strings.NewReader(samples[0]))
	decoder.DisallowUnknownFields()
	require.NoError(t, decoder.Decode(&binding))
	require.Equal(t, linear.ConnectorID, binding.ConnectorID)
	require.Equal(t, issueevents.ConnectionName, binding.ConnectionName)
	require.Equal(t, linear.IssueEventReceivedTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, issueevents.IssueCreatedTriggerBinding, binding.BindingName)
	require.Equal(t, teamBinding, binding.Configuration, "the tests use the README's binding")
	require.Contains(t, readme, webhookPath)

	curlBody := regexp.MustCompile(`(?m)^body='(.+)'$`).FindStringSubmatch(readme)
	require.Len(t, curlBody, 2)
	body := strings.Replace(curlBody[1], "NOW", strconv.FormatInt(time.Now().UnixMilli(), 10), 1)
	events := make(chan sdkgo.TriggerEvent[linear.IssueEvent], 1)
	endpoint := newIssueEndpoint(t, newExampleConnection(t), sdkgo.TriggerTargetFunc[linear.IssueEvent](
		func(_ context.Context, event sdkgo.TriggerEvent[linear.IssueEvent]) error {
			events <- event
			return nil
		}))
	endpoint.start(t)
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, body, sentinelSigningSecret, false))
	select {
	case event := <-events:
		require.Contains(t, readme, "The event ID is `"+event.ID+"`")
		require.Contains(t, readme, issueevents.ResolveFlowID(event))
		require.True(t, issueevents.AcceptIssueCreated(event))
	case <-time.After(5 * time.Second):
		t.Fatal("the README delivery was not recorded")
	}
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
