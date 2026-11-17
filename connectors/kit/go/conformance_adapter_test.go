// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v4"
const conformanceFixturePathPrefix = "/v4"

type kitConformanceContext struct {
	queryContext
	attempt       int32
	lastHeartbeat json.RawMessage
}

func (context *kitConformanceContext) Attempt() int32 { return context.attempt }

func (context *kitConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.lastHeartbeat = encoded
	}
	return err
}

func (context *kitConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.lastHeartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "kit", Name: "kit"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &kitConformanceContext{}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.lastHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = prior + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listSubscribers":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscribers(), reference),
		"getSubscriber":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscriber(), reference),
		"upsertSubscriber":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertSubscriber(), reference, scenario.CallID),
		"listTags":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTags(), reference),
		"createTag":               newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTag(), reference, scenario.CallID),
		"tagSubscriber":           newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TagSubscriber(), reference, scenario.CallID),
		"removeTagFromSubscriber": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveTagFromSubscriber(), reference, scenario.CallID),
		"listCustomFields":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCustomFields(), reference),
		"createCustomField":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateCustomField(), reference, scenario.CallID),
		"updateCustomField":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCustomField(), reference, scenario.CallID),
		"deleteCustomField":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCustomField(), reference, scenario.CallID),
		"listForms":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListForms(), reference),
		"listFormSubscribers":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListFormSubscribers(), reference),
		"addSubscriberToForm":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.AddSubscriberToForm(), reference, scenario.CallID),
		"listSequences":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSequences(), reference),
		"listSequenceSubscribers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSequenceSubscribers(), reference),
		"addSubscriberToSequence": newConformanceMutationInvocation(t, scenario.Input, context, client.AddSubscriberToSequence(), reference, scenario.CallID),
		"listTagSubscribers":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTagSubscribers(), reference),
	}}
}
