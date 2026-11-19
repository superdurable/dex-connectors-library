// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type odooConformanceContext struct {
	queryContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *odooConformanceContext) Attempt() int32 { return context.attempt }

func (context *odooConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, target)
}

func (context *odooConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	context := &odooConformanceContext{}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = append(json.RawMessage(nil), value...) })
	reference := sdkgo.ConnectionRef{Provider: "odoo", Name: "odoo"}
	client, err := New(Config{BaseURL: endpoint, Database: "mycompany"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-odoo-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
		}
		context.attempt = priorAttempt + int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"searchPartners":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchPartners(), reference),
		"listContacts":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"createContact":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), reference, scenario.CallID),
		"updateContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContact(), reference, scenario.CallID),
		"deleteContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContact(), reference, scenario.CallID),
		"listOpportunities": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOpportunities(), reference),
		"getOpportunity":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOpportunity(), reference),
		"createOpportunity": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateOpportunity(), reference, scenario.CallID),
		"updateOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateOpportunity(), reference, scenario.CallID),
		"deleteOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteOpportunity(), reference, scenario.CallID),
		"listRecords":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRecords(), reference),
		"getRecord":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRecord(), reference),
		"createRecord":      newConformanceMutationInvocation(t, scenario.Input, context, client.CreateRecord(), reference, scenario.CallID),
		"updateRecord":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateRecord(), reference, scenario.CallID),
		"deleteRecord":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteRecord(), reference, scenario.CallID),
	}}
}
