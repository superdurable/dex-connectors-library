// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shortcut

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v3"
const conformanceFixturePathPrefix = "/api/v3"

type shortcutConformanceContext struct {
	queryContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *shortcutConformanceContext) Attempt() int32 { return context.attempt }

func (context *shortcutConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func (context *shortcutConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "shortcut", Name: "shortcut"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-shortcut-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &shortcutConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.prior = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getStory":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetStory(), reference),
		"searchStories": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchStories(), reference),
		"createStory":   newConformanceMutationInvocation(t, scenario.Input, context, client.CreateStory(), reference, scenario.CallID),
		"updateStory":   newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateStory(), reference, scenario.CallID),
		"deleteStory":   newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteStory(), reference, scenario.CallID),
	}}
}
