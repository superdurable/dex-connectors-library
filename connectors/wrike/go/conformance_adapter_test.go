// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wrike

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "wrike", Name: "wrike"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-wrike-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := &taskMutationContext{queryContext: queryContext{}}
	if len(scenario.PriorHeartbeat) != 0 {
		var marker struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		mutationContext.heartbeat = append([]byte(nil), marker.Value...)
		mutationContext.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getTask":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTask(), reference),
		"listTasks":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTasks(), reference),
		"createTask": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateTask(), reference, scenario.CallID),
		"updateTask": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateTask(), reference, scenario.CallID),
		"deleteTask": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteTask(), reference, scenario.CallID),
	}}
}

type taskMutationContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (context *taskMutationContext) Attempt() int32 {
	if context.attempt == 0 {
		return 1
	}
	return context.attempt
}

func (context *taskMutationContext) RecordHeartbeat(value any) error {
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

func (context *taskMutationContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
