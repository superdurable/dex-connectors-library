// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightly

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v3.1"
const conformanceFixturePathPrefix = "/v3.1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "insightly", Name: "insightly"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listContacts-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := singledispatchtest.NewContext("insightly-conformance", "create-record")
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(marker []byte) {
		require.NoError(t, createContext.SetPersistedCheckpoint(json.RawMessage(marker)))
	})
	for attempt := int32(0); attempt < priorAttempt; attempt++ {
		createContext.AdvanceAttempt()
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			createContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listContacts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"listRecords":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRecords(), reference),
		"getRecord":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRecord(), reference),
		"searchRecords": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchRecords(), reference),
		"createRecord":  newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateRecord(), reference, scenario.CallID),
		"updateRecord":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateRecord(), reference, scenario.CallID),
		"deleteRecord":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteRecord(), reference, scenario.CallID),
	}}
}
