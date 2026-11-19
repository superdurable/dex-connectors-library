// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pandadoc

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/public/v1"
const conformanceFixturePathPrefix = "/public/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "pandadoc", Name: "pandadoc"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-pandadoc-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &pandaDocConformanceContext{}
	if len(scenario.PriorHeartbeat) != 0 {
		context.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		context.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getDocument":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetDocument(), reference),
		"listDocuments":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListDocuments(), reference),
		"updateDocument": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateDocument(), reference, scenario.CallID),
		"deleteDocument": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteDocument(), reference, scenario.CallID),
		"createDocument": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateDocument(), reference, scenario.CallID),
		"sendDocument":   newConformanceMutationInvocation(t, scenario.Input, context, client.SendDocument(), reference, scenario.CallID),
	}}
}

type pandaDocConformanceContext struct {
	queryContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *pandaDocConformanceContext) Attempt() int32 {
	if context.attempt == 0 {
		return 1
	}
	return context.attempt
}
func (context *pandaDocConformanceContext) RecordHeartbeat(value any) error {
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
func (context *pandaDocConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
