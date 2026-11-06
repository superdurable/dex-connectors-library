// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom_test

import (
  "net/http"
  "testing"

  "github.com/stretchr/testify/require"
  "github.com/superdurable/dex-connectors-library/connectors/zoom/go"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

type zoomConformanceContext struct { *zoomDexContext; attempt int32 }
func (context *zoomConformanceContext) Attempt() int32 { return context.attempt }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
  t.Helper()
  client, err := zoom.New(zoom.Config{Endpoint: endpoint}, staticZoomCredentials(), zoom.WithHTTPClient(transport))
  require.NoError(t, err)
  context := &zoomConformanceContext{zoomDexContext: newZoomDexContext("conformance"), attempt: 1}
  priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeatValue = value })
  return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) + priorAttempt }, actions: map[string]conformanceOperationInvocation{
    "listMeetings": newConformanceQueryInvocation(t, scenario.Input, context, client.ListMeetings(), zoomConnection),
    "getMeeting": newConformanceQueryInvocation(t, scenario.Input, context, client.GetMeeting(), zoomConnection),
    "createMeeting": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateMeeting(), zoomConnection, scenario.CallID),
    "updateMeeting": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateMeeting(), zoomConnection, scenario.CallID),
    "listPastMeetingParticipants": newConformanceQueryInvocation(t, scenario.Input, context, client.ListPastMeetingParticipants(), zoomConnection),
  }}
}
