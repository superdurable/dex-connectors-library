// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package firestore

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google-firestore", Name: "google-firestore"}
	credentials := Credentials{
		OAuthClientID: "synthetic-client-id", OAuthClientSecret: sdkgo.NewSecretString("synthetic-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-google-firestore-token"), RefreshToken: sdkgo.NewSecretString("synthetic-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("firestore-conformance", "provider-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listDocuments":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListDocuments(), reference),
		"getDocument":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetDocument(), reference),
		"listDocumentDetails": newConformanceQueryInvocation(t, scenario.Input, context, client.ListDocumentDetails(), reference),
		"createDocument":      newConformanceMutationInvocation(t, scenario.Input, context, client.CreateDocument(), reference, scenario.CallID),
		"putDocument":         newConformanceMutationInvocation(t, scenario.Input, context, client.PutDocument(), reference, scenario.CallID),
		"deleteDocument":      newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteDocument(), reference, scenario.CallID),
		"runDocumentQuery":    newConformanceQueryInvocation(t, scenario.Input, context, client.RunDocumentQuery(), reference),
		"listRootCollections": newConformanceQueryInvocation(t, scenario.Input, context, client.ListRootCollections(), reference),
	}}
}
