// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptrace"
	"testing"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	config := oauth2http.Config{
		AuthorizationEndpoint: "http://127.0.0.1:12345/auth", TokenEndpoint: "http://127.0.0.1:12345/token",
		Scopes: []string{"profile"}, APIBaseURL: endpoint,
	}
	credentials := sdkgo.StaticCredentialProvider[oauth2http.Credentials]{testConnection: testCredentials("conformance-access-token", "")}
	client, err := oauth2http.NewWithConformanceTransportForTest(config, credentials, &conformanceTracedTransport{transport: transport.Transport, exchanges: scenario.Exchanges}, conformanceFixedClock(t, scenario))
	require.NoError(t, err)
	// The context persists heartbeats across attempts, so the postNonIdempotent fence sees earlier claims.
	context := singledispatchtest.NewContext("oauth2-http-conformance", "send-request")
	var priorHeartbeat []byte
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { priorHeartbeat = value })
	for range priorAttempt {
		context.AdvanceAttempt()
	}
	if priorHeartbeat != nil {
		require.NoError(t, context.SetPersistedCheckpoint(json.RawMessage(priorHeartbeat)))
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"sendRequest":       newConformanceMutationInvocation(t, scenario.Input, context, client.SendRequest(), testConnection, scenario.CallID),
		"postNonIdempotent": newConformanceMutationInvocation(t, scenario.Input, context, client.PostNonIdempotent(), testConnection, scenario.CallID),
	}}
}

type conformanceTracedTransport struct {
	transport http.RoundTripper
	exchanges []conformanceExchange
	index     int
}

func (transport *conformanceTracedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := transport.index
	transport.index++
	if index < len(transport.exchanges) && transport.exchanges[index].TransportFailure == "after-send" {
		if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.WroteRequest != nil {
			trace.WroteRequest(httptrace.WroteRequestInfo{})
		}
	}
	return transport.transport.RoundTrip(request)
}
