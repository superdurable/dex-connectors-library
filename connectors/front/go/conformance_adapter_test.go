// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceFrontContext struct {
	*testDexContext
	attempt int32
}

func (context *conformanceFrontContext) Attempt() int32 { return context.attempt }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := front.New(front.Config{}, testCredentialProvider(), front.WithAPIBaseURL(endpoint), front.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &conformanceFrontContext{testDexContext: newTestDexContext("conformance"), attempt: 1}
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.heartbeat = value })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = prior + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"searchConversations": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchConversations(), frontConnection),
		"getConversation":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetConversation(), frontConnection),
		"replyToConversation": newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToConversation(), frontConnection, scenario.CallID),
		"updateConversation":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateConversation(), frontConnection, scenario.CallID),
		"findContactByEmail":  newConformanceQueryInvocation(t, scenario.Input, context, client.FindContactByEmail(), frontConnection),
	}}
}
