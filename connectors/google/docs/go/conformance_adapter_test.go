// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
    "net/http"
    "testing"

    "github.com/stretchr/testify/require"
    docs "github.com/superdurable/dex-connectors-library/connectors/google/docs/go"
    "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "", ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    t.Helper()
    client, err := docs.New(docs.Config{DocsEndpoint: endpoint, DriveEndpoint: endpoint},
        sdkgo.StaticCredentialProvider[docs.Credentials]{docsConnection: {AccessToken: sdkgo.NewSecretString(docsTestToken)}},
        docs.WithHTTPClient(transport))
    require.NoError(t, err)
    context := newDexContext("conformance")
    prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw })
    return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = prior + int32(attempt) },
        actions: map[string]conformanceOperationInvocation{
            "getDocumentText": newConformanceQueryInvocation(t, scenario.Input, context, client.GetDocumentText(), docsConnection),
            "createDocument": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateDocument(), docsConnection, scenario.CallID),
            "replaceDocumentText": newConformanceMutationInvocation(t, scenario.Input, context, client.ReplaceDocumentText(), docsConnection, scenario.CallID),
            "appendText": newConformanceMutationInvocation(t, scenario.Input, context, client.AppendText(), docsConnection, scenario.CallID),
        }}
}
