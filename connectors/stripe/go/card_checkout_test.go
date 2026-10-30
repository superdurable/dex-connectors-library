// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCreateCardCheckoutSessionUsesCardAndStableIdempotency(t *testing.T) {
	var form url.Values
	var idempotencyKey string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/checkout/sessions", request.URL.Path)
		require.NoError(t, request.ParseForm())
		form = request.Form
		idempotencyKey = request.Header.Get("Idempotency-Key")
		require.NotEmpty(t, idempotencyKey)
		_, _ = response.Write([]byte(`{"id":"cs_test_card","object":"checkout.session","url":"https://checkout.stripe.com/c/pay/cs_test_card","client_reference_id":"registration-123","payment_status":"unpaid","status":"open","currency":"usd","amount_total":7500}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	input := stripe.CreateCardCheckoutSessionInput(validCreateInput())
	result, err := sdkgo.RunMutation(newStripeDexContext("create-card-checkout"), client.CreateCardCheckoutSession(), stripeConnection, input)
	require.NoError(t, err)
	require.Equal(t, stripe.CreateCardCheckoutSessionBranchCreated, result.Branch)
	require.Equal(t, "cs_test_card", result.Value.ID)
	require.Equal(t, "card", form.Get("payment_method_types[0]"))
	require.Equal(t, "payment", form.Get("mode"))
	require.Equal(t, "7500", form.Get("line_items[0][price_data][unit_amount]"))
	require.Equal(t, "registration-123", form.Get("metadata[registration_id]"))
	require.Equal(t, "registration-123", form.Get("payment_intent_data[metadata][registration_id]"))
	require.Equal(t, string(result.Receipt.IdempotencyKey), idempotencyKey)
}

func TestCreateCardCheckoutSessionRejectsInvalidInput(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	input := stripe.CreateCardCheckoutSessionInput(validCreateInput())
	input.Currency = "USD"
	result, err := sdkgo.RunMutation(newStripeDexContext("invalid-card"), client.CreateCardCheckoutSession(), stripeConnection, input)
	require.NoError(t, err)
	require.Equal(t, stripe.CreateCardCheckoutSessionBranchDefect, result.Branch)
}

func TestCreateCardCheckoutSessionRejectsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(`{"error":{"type":"invalid_request_error"}}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunMutation(newStripeDexContext("rejected-card"), client.CreateCardCheckoutSession(), stripeConnection, stripe.CreateCardCheckoutSessionInput(validCreateInput()))
	require.NoError(t, err)
	require.Equal(t, stripe.CreateCardCheckoutSessionBranchProviderRejected, result.Branch)
}
