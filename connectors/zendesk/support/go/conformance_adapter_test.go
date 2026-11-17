// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
)

const conformanceEndpointPath = "/api/v2"
const conformanceFixturePathPrefix = "/api/v2"

type supportConformanceContext struct {
	*zendeskDexContext
	attempt   int32
	heartbeat []byte
}

func (context *supportConformanceContext) Attempt() int32 { return context.attempt }

func (context *supportConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}

func (context *supportConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := support.New(support.Config{Subdomain: testSubdomain}, testCredentialProvider(), support.WithAPIBaseURL(endpoint), support.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &supportConformanceContext{zendeskDexContext: newZendeskDexContext("conformance"), attempt: 1}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"searchTickets":          newConformanceQueryInvocation(t, scenario.Input, context, client.SearchTickets(), zendeskConnection),
		"getTicket":              newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicket(), zendeskConnection),
		"createTicket":           newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTicket(), zendeskConnection, scenario.CallID),
		"updateTicket":           newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateTicket(), zendeskConnection, scenario.CallID),
		"listTickets":            newConformanceQueryInvocation(t, scenario.Input, context, client.ListTickets(), zendeskConnection),
		"deleteTicket":           newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteTicket(), zendeskConnection, scenario.CallID),
		"recoverSuspendedTicket": newConformanceMutationInvocation(t, scenario.Input, context, client.RecoverSuspendedTicket(), zendeskConnection, scenario.CallID),
		"listTicketFields":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListTicketFields(), zendeskConnection),
		"getTicketField":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetTicketField(), zendeskConnection),
		"listUsers":              newConformanceQueryInvocation(t, scenario.Input, context, client.ListUsers(), zendeskConnection),
		"getUser":                newConformanceQueryInvocation(t, scenario.Input, context, client.GetUser(), zendeskConnection),
		"searchUsers":            newConformanceQueryInvocation(t, scenario.Input, context, client.SearchUsers(), zendeskConnection),
		"createUser":             newConformanceMutationInvocation(t, scenario.Input, context, client.CreateUser(), zendeskConnection, scenario.CallID),
		"updateUser":             newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateUser(), zendeskConnection, scenario.CallID),
		"deleteUser":             newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteUser(), zendeskConnection, scenario.CallID),
	}}
}
