// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = testSitePrefix

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newJiraClient(t, endpoint, jira.WithHTTPClient(transport))
	context := singledispatchtest.NewContext("jira-conformance", "conformance")
	priorAttempt := 0
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			Attempt int `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		require.NoError(t, context.SetPersistedCheckpoint(scenario.PriorHeartbeat))
		priorAttempt = marker.Attempt
		for range priorAttempt {
			context.AdvanceAttempt()
		}
	}
	currentAttempt := priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			for currentAttempt < priorAttempt+attempt {
				context.AdvanceAttempt()
				currentAttempt++
			}
		},
		actions: map[string]conformanceOperationInvocation{
			"searchIssues":    newConformanceQueryInvocation(t, scenario.Input, context, client.SearchIssues(), jiraConnection),
			"getIssue":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetIssue(), jiraConnection),
			"createIssue":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateIssue(), jiraConnection, scenario.CallID),
			"transitionIssue": newConformanceMutationInvocation(t, scenario.Input, context, client.TransitionIssue(), jiraConnection, scenario.CallID),
			"addComment":      newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), jiraConnection, scenario.CallID),
		}}
}
