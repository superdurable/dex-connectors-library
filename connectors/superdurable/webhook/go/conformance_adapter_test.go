// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"bytes"
	"encoding/json"
	"net/http"
<<<<<<< HEAD
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
	decoder := json.NewDecoder(bytes.NewReader(scenario.Input))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&decodedInput))
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
=======
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/go"
)

const conformanceEndpointPath = "/events"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	var input webhook.SendEventInput
	require.NoError(t, json.Unmarshal(scenario.Input, &input))
	var compact bytes.Buffer
	require.NoError(t, json.Compact(&compact, input.Payload))
	input.Payload = compact.Bytes()
	encodedInput, err := json.Marshal(input)
	require.NoError(t, err)
	deliveryURL := strings.Replace(endpoint, "http://", "https://", 1)
	if scenario.Name == "defect-no-delivery-url" {
		deliveryURL = ""
	}
	client, err := webhook.New(webhook.Config{DeliveryURL: deliveryURL},
		staticCredentials(sentinelSecret), webhook.WithHTTPClient(transport), webhook.WithClock(func() time.Time { return fixedNow }))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"sendEvent": newConformanceMutationInvocation(t, encodedInput, newStepContext("conformance"), client.SendEvent(), testConnection, scenario.CallID),
	}}
}
>>>>>>> origin/main
