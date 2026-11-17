// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

type eventbriteConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *eventbriteConformanceContext) Attempt() int32 { return context.attempt }

func (context *eventbriteConformanceContext) RecordHeartbeat(value any) error {
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

func (context *eventbriteConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "eventbrite", Name: "eventbrite"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PrivateToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &eventbriteConformanceContext{attempt: 1}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		context.attempt = int32(attempt) + priorAttempt
	}, actions: map[string]conformanceOperationInvocation{
		"listOrganizations":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrganizations(), reference),
		"listEvents":                  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListEvents(), reference),
		"getAttendee":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAttendee(), reference),
		"getEvent":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetEvent(), reference),
		"publishEvent":                newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.PublishEvent(), reference, scenario.CallID),
		"createEvent":                 newConformanceMutationInvocation(t, scenario.Input, context, client.CreateEvent(), reference, scenario.CallID),
		"copyEvent":                   newConformanceMutationInvocation(t, scenario.Input, context, client.CopyEvent(), reference, scenario.CallID),
		"updateEvent":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateEvent(), reference, scenario.CallID),
		"unpublishEvent":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnpublishEvent(), reference, scenario.CallID),
		"cancelEvent":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelEvent(), reference, scenario.CallID),
		"deleteEvent":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteEvent(), reference, scenario.CallID),
		"listAttendeesByEvent":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAttendeesByEvent(), reference),
		"listAttendeesByOrganization": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAttendeesByOrganization(), reference),
		"getOrder":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), reference),
		"listOrdersByEvent":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrdersByEvent(), reference),
		"listOrdersByOrganization":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrdersByOrganization(), reference),
	}}
}
