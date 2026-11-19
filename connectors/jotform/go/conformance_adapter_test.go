// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jotform

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type jotformConformanceContext struct {
	queryContext
	attempt       int32
	lastHeartbeat json.RawMessage
}

func (context *jotformConformanceContext) Attempt() int32 { return context.attempt }

func (context *jotformConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil {
		context.lastHeartbeat = encoded
	}
	return err
}

func (context *jotformConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.lastHeartbeat, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "jotform", Name: "jotform"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("test-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &jotformConformanceContext{}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.lastHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = prior + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listForms":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListForms(), reference),
		"getSubmission":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubmission(), reference),
		"getForm":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetForm(), reference),
		"listFormSubmissions": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListFormSubmissions(), reference),
		"createForm":          newConformanceMutationInvocation(t, scenario.Input, context, client.CreateForm(), reference, scenario.CallID),
		"updateFormTitle":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateFormTitle(), reference, scenario.CallID),
		"deleteForm":          newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteForm(), reference, scenario.CallID),
		"createSubmission":    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSubmission(), reference, scenario.CallID),
		"updateSubmission":    newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateSubmission(), reference, scenario.CallID),
		"deleteSubmission":    newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteSubmission(), reference, scenario.CallID),
	}}
}
