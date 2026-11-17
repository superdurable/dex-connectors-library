// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package creatio

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "creatio", Name: "creatio"}
	client, err := New(Config{BaseURL: endpoint, TokenEndpoint: endpoint + "/connect/token"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {
			ClientID: "sample-client-id", ClientSecret: sdkgo.NewSecretString("sample-client-secret"),
			AccessToken: sdkgo.NewSecretString("sample-access-token"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &contactConformanceContext{attempt: 1}
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
		"getContact":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"listContacts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"createContact": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), reference, scenario.CallID),
		"updateContact": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateContact(), reference, scenario.CallID),
		"deleteContact": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteContact(), reference, scenario.CallID),
	}}
}

type contactConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *contactConformanceContext) Attempt() int32 { return context.attempt }
func (context *contactConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *contactConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
