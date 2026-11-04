// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/ex/jira/" + testCloudID + "/rest/api/3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newJiraClient(t, endpoint, jira.WithHTTPClient(transport))
	context := newJiraConformanceContext()
	firstAttempt := int32(1)
	if len(scenario.PriorHeartbeat) > 0 {
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &context.previousHeartbeat))
		firstAttempt = context.previousHeartbeat.Attempt + 1
		context.attempt = firstAttempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt == 1 {
			context.attempt = firstAttempt
			return
		}
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = jiraConformanceMarker{}
		}
		context.attempt = int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"searchIssues":    newConformanceQueryInvocation(t, scenario.Input, context, client.SearchIssues(), jiraConnection),
		"getIssue":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetIssue(), jiraConnection),
		"createIssue":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateIssue(), jiraConnection, scenario.CallID),
		"transitionIssue": newConformanceMutationInvocation(t, scenario.Input, context, client.TransitionIssue(), jiraConnection, scenario.CallID),
		"addComment":      newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), jiraConnection, scenario.CallID),
	}}
}

type jiraConformanceMarker struct {
	CallID     string `json:"callId"`
	Attempt    int32  `json:"attempt"`
	State      string `json:"state"`
	ProviderID string `json:"providerId"`
}
type jiraConformanceContext struct {
	*jiraDexContext
	attempt           int32
	previousHeartbeat jiraConformanceMarker
	recordedHeartbeat jiraConformanceMarker
}

func newJiraConformanceContext() *jiraConformanceContext {
	return &jiraConformanceContext{jiraDexContext: newJiraDexContext("conformance"), attempt: 1}
}
func (context *jiraConformanceContext) Attempt() int32 { return context.attempt }
func (context *jiraConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, &context.recordedHeartbeat)
}
func (context *jiraConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if context.previousHeartbeat.CallID == "" {
		return false, nil
	}
	encoded, err := json.Marshal(context.previousHeartbeat)
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(encoded, value)
}
