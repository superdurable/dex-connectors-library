// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package harvest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	connection := sdkgo.ConnectionRef{Provider: "harvest", Name: "harvest"}
	client, err := New(Config{BaseURL: endpoint, AccountID: "123"}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-secret-key")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &harvestReplayContext{attempt: 1}
	var priorAttempt int32
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		if len(marker.Value) > 0 {
			context.heartbeat = append(json.RawMessage(nil), marker.Value...)
		} else {
			context.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		}
		priorAttempt = marker.Attempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) + priorAttempt }, actions: map[string]conformanceOperationInvocation{
		"listContacts":                     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), connection),
		"getContact":                       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), connection),
		"createContact":                    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), connection, scenario.CallID),
		"updateContact":                    newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateContact(), connection, scenario.CallID),
		"deleteContact":                    newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteContact(), connection, scenario.CallID),
		"listUsers":                        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListUsers(), connection),
		"getUser":                          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUser(), connection),
		"getCurrentUser":                   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCurrentUser(), connection),
		"createUser":                       newConformanceMutationInvocation(t, scenario.Input, context, client.CreateUser(), connection, scenario.CallID),
		"updateUser":                       newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateUser(), connection, scenario.CallID),
		"deleteUser":                       newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteUser(), connection, scenario.CallID),
		"listClients":                      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListClients(), connection),
		"getClient":                        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetClient(), connection),
		"createClient":                     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateClient(), connection, scenario.CallID),
		"updateClient":                     newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateClient(), connection, scenario.CallID),
		"deleteClient":                     newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteClient(), connection, scenario.CallID),
		"listProjects":                     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), connection),
		"getProject":                       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProject(), connection),
		"createProject":                    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateProject(), connection, scenario.CallID),
		"updateProject":                    newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateProject(), connection, scenario.CallID),
		"deleteProject":                    newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteProject(), connection, scenario.CallID),
		"listTimeEntries":                  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTimeEntries(), connection),
		"getTimeEntry":                     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTimeEntry(), connection),
		"createTimeEntryByDuration":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTimeEntryByDuration(), connection, scenario.CallID),
		"createTimeEntryByStartEnd":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTimeEntryByStartEnd(), connection, scenario.CallID),
		"updateTimeEntry":                  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTimeEntry(), connection, scenario.CallID),
		"deleteTimeEntry":                  newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTimeEntry(), connection, scenario.CallID),
		"deleteTimeEntryExternalReference": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTimeEntryExternalReference(), connection, scenario.CallID),
		"stopTimeEntry":                    newConformanceMutationInvocation(t, scenario.Input, context, client.StopTimeEntry(), connection, scenario.CallID),
	}}
}

type harvestReplayContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *harvestReplayContext) Attempt() int32 { return context.attempt }

func (context *harvestReplayContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}

func (context *harvestReplayContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

var _ dex.Context = (*harvestReplayContext)(nil)
