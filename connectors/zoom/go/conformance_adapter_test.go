// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

type conformanceZoomDexContext struct {
	*zoomDexContext
	attempt int32
}

func (context *conformanceZoomDexContext) Attempt() int32 { return context.attempt }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := zoom.New(zoom.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[zoom.Credentials]{
		zoomConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, zoom.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &conformanceZoomDexContext{zoomDexContext: newZoomDexContext("conformance"), attempt: 1}
	var initialAttempt int32
	if len(scenario.PriorHeartbeat) > 0 {
		var wrapped struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &wrapped))
		initialAttempt = wrapped.Attempt
		if len(wrapped.Value) > 0 {
			context.heartbeatValue = wrapped.Value
		} else {
			context.heartbeatValue = scenario.PriorHeartbeat
		}
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		context.attempt = initialAttempt + int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"listMeetings":                newConformanceQueryInvocation(t, scenario.Input, context, client.ListMeetings(), zoomConnection),
		"getMeeting":                  newConformanceQueryInvocation(t, scenario.Input, context, client.GetMeeting(), zoomConnection),
		"createMeeting":               newConformanceMutationInvocation(t, scenario.Input, context, client.CreateMeeting(), zoomConnection, scenario.CallID),
		"updateMeeting":               newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateMeeting(), zoomConnection, scenario.CallID),
		"listPastMeetingParticipants": newConformanceQueryInvocation(t, scenario.Input, context, client.ListPastMeetingParticipants(), zoomConnection),
	}}
}
