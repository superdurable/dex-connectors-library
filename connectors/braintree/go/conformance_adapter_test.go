// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "braintree", Name: "braintree"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PublicKey: "sample-public-key", PrivateKey: sdkgo.NewSecretString("sample-private-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	fenceContext := singledispatchtest.NewContext("braintree-conformance", "create-customer")
	if len(scenario.PriorHeartbeat) != 0 {
		var prior struct {
			Attempt int32           `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &prior))
		for attempt := int32(0); attempt < prior.Attempt; attempt++ {
			fenceContext.AdvanceAttempt()
		}
		require.NoError(t, fenceContext.SetPersistedCheckpoint(prior.Value))
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			fenceContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getTransaction":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTransaction(), reference),
		"getCustomer":                    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomer(), reference),
		"getPaymentMethod":               newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPaymentMethod(), reference),
		"getRefund":                      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRefund(), reference),
		"createCustomer":                 newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CreateCustomer(), reference, scenario.CallID),
		"updateCustomer":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCustomer(), reference, scenario.CallID),
		"deleteCustomer":                 newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCustomer(), reference, scenario.CallID),
		"vaultPaymentMethod":             newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.VaultPaymentMethod(), reference, scenario.CallID),
		"updateCreditCardCardholderName": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.UpdateCreditCardCardholderName(), reference, scenario.CallID),
		"updateCreditCardExpirationDate": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.UpdateCreditCardExpirationDate(), reference, scenario.CallID),
		"updateCreditCardBillingAddress": newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.UpdateCreditCardBillingAddress(), reference, scenario.CallID),
		"deletePaymentMethodFromVault":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeletePaymentMethodFromVault(), reference, scenario.CallID),
		"searchTransactions":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchTransactions(), reference),
		"chargePaymentMethod":            newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.ChargePaymentMethod(), reference, scenario.CallID),
		"captureTransaction":             newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.CaptureTransaction(), reference, scenario.CallID),
		"voidTransaction":                newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.VoidTransaction(), reference, scenario.CallID),
		"refundTransaction":              newConformanceMutationInvocation(t, scenario.Input, fenceContext, client.RefundTransaction(), reference, scenario.CallID),
	}}
}
