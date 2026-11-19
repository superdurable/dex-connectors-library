// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = "/api/v1"

type nimbleConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (ctx *nimbleConformanceContext) Attempt() int32 { return ctx.attempt }

func (ctx *nimbleConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(ctx.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeat, target)
}

func (ctx *nimbleConformanceContext) RecordHeartbeat(value any) error {
	if value == nil {
		ctx.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.heartbeat = encoded
	return nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	context := &nimbleConformanceContext{}
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) {
		context.heartbeat = append(json.RawMessage(nil), value...)
	})
	reference := sdkgo.ConnectionRef{Provider: "nimble", Name: "nimble"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listContactIDs-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		context.attempt = priorAttempt + int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"listContactIDs":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactIDs(), reference),
		"getContact":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"searchContactIDs":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchContactIDs(), reference),
		"createContact":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), reference, scenario.CallID),
		"updateContact":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateContact(), reference, scenario.CallID),
		"deleteContact":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteContact(), reference, scenario.CallID),
		"listContactNotes":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContactNotes(), reference),
		"getContactNote":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContactNote(), reference),
		"createContactNote": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContactNote(), reference, scenario.CallID),
		"updateContactNote": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateContactNote(), reference, scenario.CallID),
		"deleteContactNote": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteContactNote(), reference, scenario.CallID),
	}}
}
