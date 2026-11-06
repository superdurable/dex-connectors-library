// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/calendly/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type calendlyConformanceContext struct {
	*stepContext
	attempt  int32
	prior    json.RawMessage
	recorded json.RawMessage
}

func (context *calendlyConformanceContext) Attempt() int32 { return context.attempt }

func (context *calendlyConformanceContext) RecordHeartbeat(value any) error {
	if value == nil {
		context.recorded = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recorded = encoded
	return nil
}

func (context *calendlyConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.prior) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.prior, value)
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	baseTransport := transport.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	redirected := *transport
	redirected.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		cloned := request.Clone(request.Context())
		cloned.URL.Scheme, cloned.URL.Host, cloned.Host = target.Scheme, target.Host, target.Host
		return baseTransport.RoundTrip(cloned)
	})
	client := newTestClient(t, &redirected, personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
	context := &calendlyConformanceContext{stepContext: newStepContext("conformance"), attempt: 1}
	if len(scenario.PriorHeartbeat) > 0 {
		context.prior = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		context.attempt = conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) { context.prior = value }) + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.prior = append(json.RawMessage(nil), context.recorded...)
			context.recorded = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listScheduledEvents":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListScheduledEvents(), testConnection),
		"getScheduledEvent":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetScheduledEvent(), testConnection),
		"listEventInvitees":         newConformanceQueryInvocation(t, scenario.Input, context, client.ListEventInvitees(), testConnection),
		"cancelScheduledEvent":      newConformanceMutationInvocation(t, scenario.Input, context, client.CancelScheduledEvent(), testConnection, scenario.CallID),
		"createSchedulingLink":      newConformanceMutationInvocation(t, scenario.Input, context, client.CreateSchedulingLink(), testConnection, scenario.CallID),
		"createWebhookSubscription": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateWebhookSubscription(), testConnection, scenario.CallID),
	}}
}
