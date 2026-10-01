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
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/helpscout/examples/conversation-triage/flow"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly and checks the curl delivery.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 3)
	var credentials struct {
		AppID         string `json:"app_id"`
		AppSecret     string `json:"app_secret"`
		WebhookSecret string `json:"webhook_secret"`
	}
	requireStrictJSON(t, samples[0], &credentials)
	require.NotContains(t, samples[0], "access_token", "Dex Web saves no token; the application obtains it")
	var binding struct {
		ConnectorID    string                                          `json:"connectorId"`
		ConnectionName string                                          `json:"connectionName"`
		TriggerName    string                                          `json:"triggerName"`
		BindingName    string                                          `json:"bindingName"`
		Configuration  helpscout.ConversationEventTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[1], &binding)
	require.Equal(t, helpscout.ConnectorID, binding.ConnectorID)
	require.Equal(t, conversationtriage.ConnectionName, binding.ConnectionName)
	require.Equal(t, helpscout.ConversationEventTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, conversationtriage.NewConversationTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())
	var triage conversationtriage.Triage
	requireStrictJSON(t, samples[2], &triage)
	require.Equal(t, "completed", triage.Stage)
	require.Equal(t, []string{"billing", conversationtriage.TriagedTag, conversationtriage.RepeatContactTag}, triage.Tags)
	require.Contains(t, readme, conversationtriage.BuildTriageNote(triage.CustomerEmail, triage.CustomerProfileIDs, triage.OtherActiveConversationIDs))

	curlBody := regexp.MustCompile(`(?m)^body='(.+)'$`).FindStringSubmatch(readme)
	require.Len(t, curlBody, 2)
	var delivery struct {
		ID              int64  `json:"id"`
		Status          string `json:"status"`
		MailboxID       int64  `json:"mailboxId"`
		PrimaryCustomer struct {
			Email string `json:"email"`
		} `json:"primaryCustomer"`
	}
	require.NoError(t, json.Unmarshal([]byte(curlBody[1]), &delivery))
	require.Equal(t, binding.Configuration.MailboxID, delivery.MailboxID, "the sample passes the binding filter")
	require.Equal(t, "active", delivery.Status, "the sample passes AcceptNewConversation")
	require.Equal(t, triage.CustomerEmail, delivery.PrimaryCustomer.Email)
	require.Contains(t, readme, "X-HelpScout-Event: "+helpscout.WebhookEventConversationCreated)
	require.Contains(t, readme, "helpscout-convo.created-501-<body digest>")
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
