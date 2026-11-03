// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai

import (
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openai/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "openai", Name: "openai"}
	var exchangeIndex atomic.Int64
	clientTransport := *transport
	clientTransport.Transport = conformanceTraceTransport{delegate: transport.Transport, scenario: scenario, index: &exchangeIndex}
	client, err := New(Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("test-secret-key")},
	}, WithHTTPClient(&clientTransport))
	require.NoError(t, err)
	context := testsupport.NewDexContext("conformance-flow", "openai-response")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		context.AttemptNo = int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"createResponse":   newConformanceMutationInvocation(t, scenario.Input, context, client.CreateResponse(), reference, scenario.CallID),
		"retrieveResponse": newConformanceQueryInvocation(t, scenario.Input, context, client.RetrieveResponse(), reference),
	}}
}

type conformanceTraceTransport struct {
	delegate http.RoundTripper
	scenario conformanceScenario
	index    *atomic.Int64
}

func (transport conformanceTraceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := int(transport.index.Add(1) - 1)
	if index < len(transport.scenario.Exchanges) {
		trace := httptrace.ContextClientTrace(request.Context())
		if trace != nil {
			switch transport.scenario.Exchanges[index].TransportFailure {
			case "before-send":
				if trace.GetConn != nil {
					trace.GetConn(request.URL.Host)
				}
			case "after-send":
				if trace.WroteHeaders != nil {
					trace.WroteHeaders()
				}
			}
		}
	}
	return transport.delegate.RoundTrip(request)
}
