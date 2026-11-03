// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := hiver.New(
		hiver.Config{RequestIntervalMilliseconds: 1}, testCredentialProvider(),
		hiver.WithAPIBaseURL(endpoint), hiver.WithHTTPClient(transport),
	)
	require.NoError(t, err)
	context := newHiverDexContext("conformance")
	initialAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) {
		context.previousHeartbeat = raw
	})
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
		}
		context.attempt = int32(attempt) + initialAttempt
	}, actions: map[string]conformanceOperationInvocation{
		"listInboxes":        newConformanceQueryInvocation(t, scenario.Input, context, client.ListInboxes(), hiverConnection),
		"listConversations":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListConversations(), hiverConnection),
		"getConversation":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetConversation(), hiverConnection),
		"updateConversation": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateConversation(), hiverConnection, scenario.CallID),
		"addNote":            newConformanceMutationInvocation(t, scenario.Input, context, client.AddNote(), hiverConnection, scenario.CallID),
		"createSharedDraft":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSharedDraft(), hiverConnection, scenario.CallID),
	}}
}
