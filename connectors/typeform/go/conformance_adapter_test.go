// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/typeform/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newTestClient(t, transport, personalAccessTokenCredentials(sentinelSecret), typeform.Config{})
	context := newStepContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listForms":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListForms(), testConnection),
		"getForm":       newConformanceQueryInvocation(t, scenario.Input, context, client.GetForm(), testConnection),
		"listResponses": newConformanceQueryInvocation(t, scenario.Input, context, client.ListResponses(), testConnection),
		"upsertWebhook": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertWebhook(), testConnection, scenario.CallID),
	}}
}
