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
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	inviteerecorder "github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly and checks the curl delivery.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 2)
	var binding struct {
		ConnectorID    string                                            `json:"connectorId"`
		ConnectionName string                                            `json:"connectionName"`
		TriggerName    string                                            `json:"triggerName"`
		BindingName    string                                            `json:"bindingName"`
		Configuration  calendly.InviteeEventReceivedTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[0], &binding)
	require.Equal(t, calendly.ConnectorID, binding.ConnectorID)
	require.Equal(t, inviteerecorder.ConnectionName, binding.ConnectionName)
	require.Equal(t, calendly.InviteeEventReceivedTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, inviteerecorder.InviteeCreatedTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())
	var recorded inviteerecorder.RecordedScheduledEvent
	requireStrictJSON(t, samples[1], &recorded)
	require.Equal(t, calendly.GetScheduledEventBranchFound, recorded.Branch)

	curlBody := regexp.MustCompile(`(?m)^body='(.+)'$`).FindStringSubmatch(readme)
	require.Len(t, curlBody, 2)
	var delivery struct {
		Event   string `json:"event"`
		Payload struct {
			URI            string `json:"uri"`
			Event          string `json:"event"`
			ScheduledEvent struct {
				URI       string `json:"uri"`
				EventType string `json:"event_type"`
			} `json:"scheduled_event"`
		} `json:"payload"`
	}
	require.NoError(t, json.Unmarshal([]byte(curlBody[1]), &delivery))
	require.Equal(t, calendly.WebhookEventInviteeCreated, delivery.Event)
	require.Equal(t, binding.Configuration.EventTypeURI, delivery.Payload.ScheduledEvent.EventType, "the sample passes the binding filter")
	require.Equal(t, recorded.ScheduledEvent.URI, delivery.Payload.ScheduledEvent.URI)
	require.Equal(t, delivery.Payload.Event, delivery.Payload.ScheduledEvent.URI)
	eventID := "invitee.created:EVENT0001:INVITEE01"
	require.Equal(t, "https://api.calendly.com/scheduled_events/EVENT0001/invitees/INVITEE01", delivery.Payload.URI)
	require.Contains(t, readme, inviteerecorder.ResolveFlowID(sdkgo.TriggerEvent[calendly.InviteeEvent]{ID: eventID}))
	require.Contains(t, readme, webhookPath)
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
