// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "razorpay", Name: "razorpay"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {KeyID: sdkgo.NewSecretString("rzp_test_1234567890"), KeySecret: sdkgo.NewSecretString("sample-razorpay-secret")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := singledispatchtest.NewContext("razorpay-conformance", "create-order")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getPayment":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPayment(), reference),
		"getOrder":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), reference),
		"listOrders":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrders(), reference),
		"createOrder":    newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateOrder(), reference, scenario.CallID),
		"listPayments":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPayments(), reference),
		"capturePayment": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CapturePayment(), reference, scenario.CallID),
	}}
}
