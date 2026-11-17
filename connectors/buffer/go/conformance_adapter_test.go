// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buffer

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type bufferConformanceContext struct {
	queryContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *bufferConformanceContext) Attempt() int32 { return context.attempt }

func (context *bufferConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}

func (context *bufferConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "buffer", Name: "buffer"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &bufferConformanceContext{attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getChannel":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetChannel(), reference),
		"listOrganizations": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrganizations(), reference),
		"listChannels":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListChannels(), reference),
		"getPost":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPost(), reference),
		"listPosts":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPosts(), reference),
		"createPost":        newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePost(), reference, scenario.CallID),
		"editPost":          newConformanceMutationInvocation(t, scenario.Input, context, client.EditPost(), reference, scenario.CallID),
		"deletePost":        newConformanceMutationInvocation(t, scenario.Input, context, client.DeletePost(), reference, scenario.CallID),
	}}
}
