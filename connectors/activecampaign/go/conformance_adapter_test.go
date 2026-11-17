// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/3"
const conformanceFixturePathPrefix = "/api/3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "activecampaign", Name: "activecampaign"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listTags":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTags(), reference),
		"getTag":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTag(), reference),
		"createTag":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTag(), reference, scenario.CallID),
		"updateTag":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateTag(), reference, scenario.CallID),
		"deleteTag":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteTag(), reference, scenario.CallID),
		"listLists":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLists(), reference),
		"getList":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetList(), reference),
		"listContacts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"syncContact":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SyncContact(), reference, scenario.CallID),
		"updateContact": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContact(), reference, scenario.CallID),
	}}
}
