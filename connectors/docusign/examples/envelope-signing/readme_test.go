// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	envelopesigning "github.com/superdurable/dex-connectors-library/connectors/docusign/examples/envelope-signing/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestREADMESamplesMatchTheExample decodes every JSON sample strictly and delivers the curl body.
func TestREADMESamplesMatchTheExample(t *testing.T) {
	readme := readFile(t, "README.md")
	samples := fencedBlocks(readme, "json")
	require.Len(t, samples, 3)
	var binding struct {
		ConnectorID    string                                             `json:"connectorId"`
		ConnectionName string                                             `json:"connectionName"`
		TriggerName    string                                             `json:"triggerName"`
		BindingName    string                                             `json:"bindingName"`
		Configuration  docusign.EnvelopeEventReceivedTriggerConfiguration `json:"configuration"`
	}
	requireStrictJSON(t, samples[0], &binding)
	require.Equal(t, docusign.ConnectorID, binding.ConnectorID)
	require.Equal(t, envelopesigning.ConnectionName, binding.ConnectionName)
	require.Equal(t, docusign.EnvelopeEventReceivedTriggerDefinition.Trigger.TriggerName, binding.TriggerName)
	require.Equal(t, envelopesigning.EnvelopeOutcomeTriggerBinding, binding.BindingName)
	require.NoError(t, binding.Configuration.Validate())

	var request envelopesigning.SigningRequest
	requireStrictJSON(t, samples[1], &request)
	require.Contains(t, readme, "`"+envelopesigning.FlowIDForRequest(request.RequestID)+"`")
	require.Equal(t, "Customer", request.Signers[0].RoleName)

	var outcome envelopesigning.SigningState
	requireStrictJSON(t, samples[2], &outcome)
	require.Equal(t, request, outcome.Request)
	require.Equal(t, envelopesigning.StatusCompleted, outcome.Status)
	require.Len(t, outcome.Document.SHA256, 64)
	require.Equal(t, []string{outcome.EnvelopeID + ":" + docusign.ConnectEventEnvelopeCompleted}, outcome.ReceivedEventIDs)
	require.Contains(t, readme, connectPath)
}

// TestREADMEConnectSampleIsAcceptedByTheEndpoint delivers the README's curl body with its envelope ID.
func TestREADMEConnectSampleIsAcceptedByTheEndpoint(t *testing.T) {
	readme := readFile(t, "README.md")
	body := regexp.MustCompile(`(?m)^body='(.+)'$`).FindStringSubmatch(readme)
	envelopeID := regexp.MustCompile(`(?m)^envelope=(\S+)$`).FindStringSubmatch(readme)
	require.Len(t, body, 2)
	require.Len(t, envelopeID, 2)
	delivered := make(chan sdkgo.TriggerEvent[docusign.EnvelopeEvent], 1)
	endpoint := newConnectEndpoint(t, newExampleConnection(t, newFakeDocuSign(t)),
		sdkgo.TriggerTargetFunc[docusign.EnvelopeEvent](func(_ context.Context, event sdkgo.TriggerEvent[docusign.EnvelopeEvent]) error {
			delivered <- event
			return nil
		}))
	require.Equal(t, http.StatusOK, endpoint.deliver(t, strings.Replace(body[1], "ENVELOPE", envelopeID[1], 1), sentinelHMACKey))
	select {
	case event := <-delivered:
		require.Equal(t, envelopeID[1]+":"+docusign.ConnectEventEnvelopeDeclined, event.ID)
		require.True(t, envelopesigning.AcceptEnvelopeEvent(event))
		require.Equal(t, envelopesigning.FlowIDForRequest("opp-123"), envelopesigning.ResolveFlowID(event))
	case <-time.After(5 * time.Second):
		t.Fatal("the README sample was not delivered")
	}
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
