// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"encoding/json"
	"net"
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
	transport.Transport = &conformanceDialTransport{exchanges: scenario.Exchanges, underlying: transport.Transport}
	client, err := intercom.New(intercom.Config{}, sdkgo.StaticCredentialProvider[intercom.Credentials]{
		intercomConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)},
	}, intercom.WithAPIBaseURL(endpoint), intercom.WithHTTPClient(transport), intercom.WithClock(func() time.Time {
		return time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC)
	}))
	require.NoError(t, err)
	context := newTestDexContext("conformance")
	if len(scenario.PriorHeartbeat) != 0 {
		var previous struct {
			CallID string `json:"callId"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &previous))
		require.NoError(t, context.RecordHeartbeat(map[string]any{
			"intercomReplyDispatchedCallId": previous.CallID,
			"intercomReplyDispatchedAt":     time.Date(2026, time.October, 30, 0, 0, 0, 0, time.UTC).Unix(),
		}))
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchConversations":     newConformanceQueryInvocation(t, scenario.Input, context, client.SearchConversations(), intercomConnection),
		"getConversation":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetConversation(), intercomConnection),
		"replyToConversation":     newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToConversation(), intercomConnection, scenario.CallID),
		"updateConversationState": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateConversationState(), intercomConnection, scenario.CallID),
		"findContactByEmail":      newConformanceQueryInvocation(t, scenario.Input, context, client.FindContactByEmail(), intercomConnection),
	}}
}

type conformanceDialTransport struct {
	exchanges    []conformanceExchange
	underlying   http.RoundTripper
	requestIndex int
}

func (transport *conformanceDialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := transport.requestIndex
	transport.requestIndex++
	response, err := transport.underlying.RoundTrip(request)
	if err != nil && index < len(transport.exchanges) && transport.exchanges[index].TransportFailure == "before-send" {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: err}
	}
	return response, err
}
