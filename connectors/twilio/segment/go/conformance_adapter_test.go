// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package segment

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

type sourceConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *sourceConformanceContext) Attempt() int32 { return context.attempt }
func (context *sourceConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *sourceConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "segment", Name: "segment"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-segment-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &sourceConformanceContext{attempt: 1, heartbeat: append(json.RawMessage(nil), scenario.PriorHeartbeat...)}
	priorAttempt := int32(0)
	if len(scenario.PriorHeartbeat) > 0 {
		var prior struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		priorAttempt = prior.Attempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt+1) + priorAttempt }, actions: map[string]conformanceOperationInvocation{
		"getSource":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSource(), reference),
		"listSources":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListSources(), reference),
		"createSource": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSource(), reference, scenario.CallID),
		"updateSource": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateSource(), reference, scenario.CallID),
		"deleteSource": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteSource(), reference, scenario.CallID),
	}}
}
