// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := intercom.New(intercom.Config{}, sdkgo.StaticCredentialProvider[intercom.Credentials]{
		intercomConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, intercom.WithAPIBaseURL(endpoint), intercom.WithHTTPClient(transport), intercom.WithClock(func() time.Time {
		return time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	}))
	require.NoError(t, err)
	context := newTestDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchConversations":     newConformanceQueryInvocation(t, scenario.Input, context, client.SearchConversations(), intercomConnection),
		"getConversation":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetConversation(), intercomConnection),
		"replyToConversation":     newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToConversation(), intercomConnection, scenario.CallID),
		"updateConversationState": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateConversationState(), intercomConnection, scenario.CallID),
		"findContactByEmail":      newConformanceQueryInvocation(t, scenario.Input, context, client.FindContactByEmail(), intercomConnection),
	}}
}
