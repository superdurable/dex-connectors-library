// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	connection := sdkgo.ConnectionRef{Provider: "close", Name: "close"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		connection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := &closeCreateConformanceContext{attempt: 1}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { createContext.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { createContext.attempt = prior + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listCalls":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCalls(), connection),
		"getCall":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCall(), connection),
		"logCall":           newConformanceMutationInvocation(t, scenario.Input, createContext, client.LogCall(), connection, scenario.CallID),
		"updateCall":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCall(), connection, scenario.CallID),
		"deleteCall":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCall(), connection, scenario.CallID),
		"listNotes":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListNotes(), connection),
		"getNote":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetNote(), connection),
		"createNote":        newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateNote(), connection, scenario.CallID),
		"updateNote":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateNote(), connection, scenario.CallID),
		"deleteNote":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteNote(), connection, scenario.CallID),
		"listTasks":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTasks(), connection),
		"getTask":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTask(), connection),
		"createTask":        newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateTask(), connection, scenario.CallID),
		"updateTask":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateTask(), connection, scenario.CallID),
		"deleteTask":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteTask(), connection, scenario.CallID),
		"listOpportunities": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOpportunities(), connection),
		"getOpportunity":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOpportunity(), connection),
		"createOpportunity": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateOpportunity(), connection, scenario.CallID),
		"updateOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateOpportunity(), connection, scenario.CallID),
		"deleteOpportunity": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteOpportunity(), connection, scenario.CallID),
		"createLead":        newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateLead(), connection, scenario.CallID),
		"createContact":     newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateContact(), connection, scenario.CallID),
		"listLeads":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLeads(), connection),
		"getLead":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLead(), connection),
		"updateLead":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateLead(), connection, scenario.CallID),
		"deleteLead":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteLead(), connection, scenario.CallID),
		"listContacts":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), connection),
		"getContact":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), connection),
		"updateContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateContact(), connection, scenario.CallID),
		"deleteContact":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteContact(), connection, scenario.CallID),
	}}
}

type closeCreateConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *closeCreateConformanceContext) Attempt() int32 { return context.attempt }

func (context *closeCreateConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *closeCreateConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
