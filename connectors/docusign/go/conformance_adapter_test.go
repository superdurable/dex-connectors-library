// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := docusign.New(docusign.Config{MaxDocumentBytes: 64}, staticCredentials(productionCredentials()),
		docusign.WithHTTPClient(transport), docusign.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	context := newTestDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"createEnvelopeFromTemplate": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateEnvelopeFromTemplate(), testConnection, scenario.CallID),
		"getEnvelope":                newConformanceQueryInvocation(t, scenario.Input, context, client.GetEnvelope(), testConnection),
		"listEnvelopeRecipients":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListEnvelopeRecipients(), testConnection),
		"voidEnvelope":               newConformanceMutationInvocation(t, scenario.Input, context, client.VoidEnvelope(), testConnection, scenario.CallID),
		"downloadCombinedDocument":   newConformanceQueryInvocation(t, scenario.Input, context, client.DownloadCombinedDocument(), testConnection),
	}}
}
