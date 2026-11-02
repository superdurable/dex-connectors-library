// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/stripe/go"
)

const conformanceEndpointPath = "/v1"
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client := newStripeClient(t, endpoint, stripe.Config{}, stripe.WithHTTPClient(transport))
	context := newStripeDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"createCardCheckoutSession": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCardCheckoutSession(), stripeConnection, scenario.CallID),
		"createACHCheckoutSession":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateACHCheckoutSession(), stripeConnection, scenario.CallID),
		"getCheckoutSession":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetCheckoutSession(), stripeConnection),
		"getPaymentIntent":          newConformanceQueryInvocation(t, scenario.Input, context, client.GetPaymentIntent(), stripeConnection),
		"createRefund":              newConformanceMutationInvocation(t, scenario.Input, context, client.CreateRefund(), stripeConnection, scenario.CallID),
	}}
}
