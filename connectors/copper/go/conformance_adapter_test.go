// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/developer_api/v1"
const conformanceFixturePathPrefix = "/developer_api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "copper", Name: "copper"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token"), UserEmail: sdkgo.NewSecretString("user@example.com")}}
	if scenario.Name == "listLeadStatuses-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &personConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.heartbeat = append([]byte(nil), scenario.PriorHeartbeat...)
		var prior struct {
			Attempt int `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(context.heartbeat, &prior))
		context.attempt = int32(prior.Attempt + 1)
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listLeadStatuses": newConformanceQueryInvocation(t, scenario.Input, context, client.ListLeadStatuses(), reference),
		"getLead":          newConformanceQueryInvocation(t, scenario.Input, context, client.GetLead(), reference),
		"createPerson":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePerson(), reference, scenario.CallID),
		"getPerson":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetPerson(), reference),
		"updatePerson":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpdatePerson(), reference, scenario.CallID),
		"deletePerson":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeletePerson(), reference, scenario.CallID),
	}}
}

type personConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *personConformanceContext) Attempt() int32 { return context.attempt }
func (context *personConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.heartbeat = encoded
	}
	return err
}
func (context *personConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
