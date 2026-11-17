// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "constantcontact", Name: "constantcontact"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			OAuthClientID: "app-id", OAuthClientSecret: sdkgo.NewSecretString("app-secret"),
			AccessToken: sdkgo.NewSecretString("sample-access-token"), RefreshToken: sdkgo.NewSecretString("sample-refresh-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &listConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.heartbeat = append([]byte(nil), scenario.PriorHeartbeat...)
		var prior struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(context.heartbeat, &prior))
		context.attempt = prior.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createContact": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), reference, scenario.CallID),
		"getContact":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetContact(), reference),
		"listContacts":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListContacts(), reference),
		"deleteContact":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteContact(), reference, scenario.CallID),
		"getContactList":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetContactList(), reference),
		"listContactLists":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListContactLists(), reference),
		"createContactList": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContactList(), reference, scenario.CallID),
		"updateContactList": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateContactList(), reference, scenario.CallID),
		"deleteContactList": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteContactList(), reference, scenario.CallID),
	}}
}

type listConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *listConformanceContext) Attempt() int32 { return context.attempt }
func (context *listConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *listConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
