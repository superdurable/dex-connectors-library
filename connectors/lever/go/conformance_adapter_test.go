// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lever

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
	attemptContext := &conformanceAttemptContext{attempt: 1}
	reference := sdkgo.ConnectionRef{Provider: "lever", Name: "lever"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-lever-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { attemptContext.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"getOpportunity":             newConformanceQueryInvocation(t, scenario.Input, attemptContext, client.GetOpportunity(), reference),
		"listOpportunities":          newConformanceQueryInvocation(t, scenario.Input, attemptContext, client.ListOpportunities(), reference),
		"updateOpportunityStage":     newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.UpdateOpportunityStage(), reference, scenario.CallID),
		"setOpportunityArchiveState": newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.SetOpportunityArchiveState(), reference, scenario.CallID),
		"createOpportunity":          newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.CreateOpportunity(), reference, scenario.CallID),
		"getPosting":                 newConformanceQueryInvocation(t, scenario.Input, attemptContext, client.GetPosting(), reference),
		"listPostings":               newConformanceQueryInvocation(t, scenario.Input, attemptContext, client.ListPostings(), reference),
		"createPosting":              newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.CreatePosting(), reference, scenario.CallID),
		"updatePosting":              newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.UpdatePosting(), reference, scenario.CallID),
		"getNote":                    newConformanceQueryInvocation(t, scenario.Input, attemptContext, client.GetNote(), reference),
		"listNotes":                  newConformanceQueryInvocation(t, scenario.Input, attemptContext, client.ListNotes(), reference),
		"createNote":                 newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.CreateNote(), reference, scenario.CallID),
		"updateNote":                 newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.UpdateNote(), reference, scenario.CallID),
		"deleteNote":                 newConformanceMutationInvocation(t, scenario.Input, attemptContext, client.DeleteNote(), reference, scenario.CallID),
	}}
}

type conformanceAttemptContext struct {
	queryContext
	attempt       int32
	lastHeartbeat json.RawMessage
}

func (context *conformanceAttemptContext) Attempt() int32 { return context.attempt }

func (context *conformanceAttemptContext) RecordHeartbeat(value any) error {
	if value == nil {
		context.lastHeartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.lastHeartbeat = encoded
	return nil
}

func (context *conformanceAttemptContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.lastHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.lastHeartbeat, target)
}
