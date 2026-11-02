// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptrace"
	"testing"

	"github.com/stretchr/testify/require"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	if len(scenario.Exchanges) == 1 && scenario.Exchanges[0].TransportFailure == "after-send" {
		transport.Transport = conformanceWrittenRequestTransport{transport.Transport}
	}
	var decodedInput any
	require.NoError(t, json.Unmarshal(scenario.Input, &decodedInput))
	canonicalInput, err := json.Marshal(decodedInput)
	require.NoError(t, err)
	client := newSendingClient(t, "https://127.0.0.1:12345/hooks?tenant=forms", standardSecret, transport)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"sendEvent": newConformanceMutationInvocation(t, canonicalInput, newStepContext("conformance"), client.SendEvent(), testConnection, scenario.CallID),
	}}
}

type conformanceWrittenRequestTransport struct{ delegate http.RoundTripper }

func (transport conformanceWrittenRequestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.WroteRequest != nil {
		trace.WroteRequest(httptrace.WroteRequestInfo{})
	}
	return transport.delegate.RoundTrip(request)
}
