// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type keapConformanceContext struct {
	queryContext
	attempt       int32
	lastHeartbeat json.RawMessage
}

func (context *keapConformanceContext) Attempt() int32 { return context.attempt }

func (context *keapConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.lastHeartbeat = encoded
	}
	return err
}

func (context *keapConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.lastHeartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "keap", Name: "keap"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PersonalAccessToken: sdkgo.NewSecretString("sample-keap-personal-access-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &keapConformanceContext{}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.lastHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = prior + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getContact":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"listContacts":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"upsertContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertContact(), reference, scenario.CallID),
		"deleteContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContact(), reference, scenario.CallID),
		"getCompany":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCompany(), reference),
		"listCompanies":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCompanies(), reference),
		"createCompany":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCompany(), reference, scenario.CallID),
		"updateCompany":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCompany(), reference, scenario.CallID),
		"deleteCompany":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCompany(), reference, scenario.CallID),
		"getContactNote":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContactNote(), reference),
		"listContactNotes":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactNotes(), reference),
		"createContactNote": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContactNote(), reference, scenario.CallID),
		"updateContactNote": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContactNote(), reference, scenario.CallID),
		"deleteContactNote": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContactNote(), reference, scenario.CallID),
		"listContactTags":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactTags(), reference),
		"applyContactTag":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.ApplyContactTag(), reference, scenario.CallID),
		"removeContactTag":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveContactTag(), reference, scenario.CallID),
		"getOrder":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), reference),
		"listOrders":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrders(), reference),
		"createOrder":       newConformanceMutationInvocation(t, scenario.Input, context, client.CreateOrder(), reference, scenario.CallID),
		"updateOrderTitle":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateOrderTitle(), reference, scenario.CallID),
		"deleteOrder":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteOrder(), reference, scenario.CallID),
	}}
}
