// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	stepName := scenario.Name
	if strings.Contains(stepName, " ") {
		stepName = "conformance"
	}
	context := newTestDexContext(stepName)
	var initialAttempt int32
	if len(scenario.PriorHeartbeat) > 0 {
		var prior struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		initialAttempt = prior.Attempt
		checkpoint, marshalErr := json.Marshal(map[string]any{"isDispatched": true, "dispatchedAt": fixedNow})
		require.NoError(t, marshalErr)
		context.heartbeatValue = checkpoint
	}
	config := docusign.Config{MaxDocumentBytes: 64}
	if scenario.Name == "download-combined-document-too-large" {
		config.MaxDocumentBytes = 10
	}
	client, err := docusign.New(config, staticCredentials(productionCredentials()),
		docusign.WithHTTPClient(transport), docusign.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = initialAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"createEnvelopeFromTemplate": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateEnvelopeFromTemplate(), testConnection, scenario.CallID),
		"getEnvelope":                newConformanceQueryInvocation(t, scenario.Input, context, client.GetEnvelope(), testConnection),
		"listEnvelopeRecipients":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListEnvelopeRecipients(), testConnection),
		"voidEnvelope":               newConformanceMutationInvocation(t, scenario.Input, context, client.VoidEnvelope(), testConnection, scenario.CallID),
		"downloadCombinedDocument":   newConformanceQueryInvocation(t, scenario.Input, context, client.DownloadCombinedDocument(), testConnection),
	}}
}
