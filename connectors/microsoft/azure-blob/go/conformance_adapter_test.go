// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package azureblob

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
	"time"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft-azure-blob", Name: "microsoft-azure-blob"}
	credentials := Credentials{AccountKey: sdkgo.NewSecretString("c2VjcmV0LXRlc3Qta2V5")}
	client, err := New(Config{Endpoint: endpoint, AccountName: "exampleacct"}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport), signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listContainers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContainers(), reference),
		"getContainer": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContainer(), reference),
		"createContainer": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateContainer(), reference, scenario.CallID),
		"deleteContainer": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContainer(), reference, scenario.CallID),
		"getBlob":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetBlob(), reference),
		"listBlobs":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListBlobs(), reference),
		"putBlob":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PutBlob(), reference, scenario.CallID),
		"deleteBlob": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteBlob(), reference, scenario.CallID),
	}}
}
