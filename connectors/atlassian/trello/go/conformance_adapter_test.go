// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/1"
const conformanceFixturePathPrefix = "/1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := trello.New(trello.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[trello.Credentials]{
		trelloConnection: {APIKey: sdkgo.NewSecretString(testAPIKey), Token: sdkgo.NewSecretString(testToken)},
	}, trello.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newTrelloDexContext("conformance")
	initialAttempt := int32(1)
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			CallID  string `json:"callId"`
			Attempt int32  `json:"attempt"`
			State   string `json:"state"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		initialAttempt = marker.Attempt + 1
		context.previousHeartbeat, err = json.Marshal(map[string]any{
			"trelloDispatchedCallId": marker.CallID, "trelloAttempt": marker.Attempt, "trelloCanRetry": marker.State == "canResend" || marker.State == "notSent",
		})
		require.NoError(t, err)
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt == 1 {
			context.attempt = initialAttempt
			return
		}
		if attempt > 1 && int32(attempt) > context.attempt {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = nil
		}
		context.attempt = int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"listCards":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListCards(), trelloConnection),
		"getCard":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetCard(), trelloConnection),
		"createCard": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCard(), trelloConnection, scenario.CallID),
		"updateCard": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCard(), trelloConnection, scenario.CallID),
		"addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), trelloConnection, scenario.CallID),
	}}
}
