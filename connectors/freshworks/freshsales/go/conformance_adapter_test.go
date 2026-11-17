// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/crm/sales/api"
const conformanceFixturePathPrefix = "/crm/sales/api"

type freshsalesConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *freshsalesConformanceContext) Attempt() int32 { return context.attempt }
func (context *freshsalesConformanceContext) RecordHeartbeat(value any) error {
	if value == nil {
		context.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *freshsalesConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "freshsales", Name: "freshsales"}
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-token")}
	if scenario.Name == "listContactViews-defect" || scenario.Name == "listAccountViews-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &freshsalesConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) + priorAttempt }, actions: map[string]conformanceOperationInvocation{
		"listContactViews": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactViews(), reference),
		"getContact":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"listContacts":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"createContact":    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), reference, scenario.CallID),
		"updateContact":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContact(), reference, scenario.CallID),
		"deleteContact":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContact(), reference, scenario.CallID),
		"listAccountViews": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccountViews(), reference),
		"getAccount":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccount(), reference),
		"listAccounts":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"createAccount":    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateAccount(), reference, scenario.CallID),
		"updateAccount":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAccount(), reference, scenario.CallID),
		"deleteAccount":    newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteAccount(), reference, scenario.CallID),
	}}
}
