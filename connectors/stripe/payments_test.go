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

func TestGetPaymentIntentReadsRefundedAmountAndCustomerEmailFromLatestCharge(t *testing.T) {
	var expand string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/payment_intents/pi_123", request.URL.Path)
		expand = request.URL.Query().Get("expand[]")
		_, _ = response.Write([]byte(`{"id":"pi_123","object":"payment_intent","amount":7500,"currency":"usd","status":"succeeded","receipt_email":null,"latest_charge":{"id":"ch_123","object":"charge","amount_refunded":2500,"billing_details":{"email":"billing@example.com"}}}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunQuery(newStripeDexContext("get-intent"), client.GetPaymentIntent(), stripeConnection, stripe.GetPaymentIntentInput{PaymentIntentID: "pi_123"})
	require.NoError(t, err)
	require.Equal(t, stripe.GetPaymentIntentBranchFound, result.Branch)
	require.Equal(t, "latest_charge", expand)
	require.Equal(t, stripe.PaymentIntent{
		ID: "pi_123", Amount: 7500, Currency: "usd", Status: "succeeded", AmountRefunded: 2500,
		CustomerEmail: "billing@example.com", LatestChargeID: "ch_123",
	}, result.Value)
}

func TestGetPaymentIntentPrefersReceiptEmailAndToleratesMissingCharge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"id":"pi_456","object":"payment_intent","amount":900,"currency":"eur","status":"requires_payment_method","receipt_email":"receipt@example.com","latest_charge":null}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunQuery(newStripeDexContext("get-intent-no-charge"), client.GetPaymentIntent(), stripeConnection, stripe.GetPaymentIntentInput{PaymentIntentID: "pi_456"})
	require.NoError(t, err)
	require.Equal(t, stripe.GetPaymentIntentBranchFound, result.Branch)
	require.Equal(t, stripe.PaymentIntent{ID: "pi_456", Amount: 900, Currency: "eur", Status: "requires_payment_method", CustomerEmail: "receipt@example.com"}, result.Value)
}

func TestGetPaymentIntentClassifiesFailures(t *testing.T) {
	testCases := []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		failure sdkgo.FailureKind
		message string
	}{
		{"missing", http.StatusNotFound, `{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such payment_intent: 'pi_gone'"}}`, stripe.GetPaymentIntentBranchNotFound, sdkgo.FailureNotFound, "resource_missing"},
		{"unauthorized", http.StatusUnauthorized, `{"error":{"type":"invalid_request_error","message":"Invalid API Key provided: sk_test_****abcd"}}`, stripe.GetPaymentIntentBranchProviderRejected, sdkgo.FailureAuthentication, "Stripe rejected the request"},
		{"malformed", http.StatusOK, `{"id":"cs_123","object":"checkout.session"}`, stripe.GetPaymentIntentBranchInvalidResponse, sdkgo.FailureProtocol, "invalid PaymentIntent"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(testCase.status)
				_, _ = response.Write([]byte(testCase.body))
			}))
			defer server.Close()
			client := newStripeClient(t, server.URL, stripe.Config{})
			result, err := sdkgo.RunQuery(newStripeDexContext("get-intent-"+testCase.name), client.GetPaymentIntent(), stripeConnection, stripe.GetPaymentIntentInput{PaymentIntentID: "pi_gone"})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.failure, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, testCase.message)
			require.NotContains(t, result.Failure.Message, "sk_test")
		})
	}
}

func TestGetPaymentIntentRejectsMalformedIdentifierBeforeCallingStripe(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	for _, identifier := range []string{"", "cs_123", "pi_1/refunds"} {
		result, err := sdkgo.RunQuery(newStripeDexContext("get-intent-invalid"), client.GetPaymentIntent(), stripeConnection, stripe.GetPaymentIntentInput{PaymentIntentID: identifier})
		require.NoError(t, err)
		require.Equal(t, stripe.GetPaymentIntentBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
}

func TestCreateRefundSendsFormFieldsAndStableIdempotencyKey(t *testing.T) {
	var form url.Values
	var idempotencyKey string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/refunds", request.URL.Path)
		require.NoError(t, request.ParseForm())
		form = request.Form
		idempotencyKey = request.Header.Get("Idempotency-Key")
		response.Header().Set("Request-Id", "req_refund")
		_, _ = response.Write([]byte(`{"id":"re_123","object":"refund","status":"pending","amount":2500,"currency":"usd"}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunMutation(newStripeDexContext("refund"), client.CreateRefund(), stripeConnection, validRefundInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateRefundBranchRefunded, result.Branch)
	require.Equal(t, stripe.Refund{RefundID: "re_123", Status: "pending", Amount: 2500, Currency: "usd"}, result.Value)
	require.NotEmpty(t, idempotencyKey)
	require.Equal(t, string(result.Receipt.IdempotencyKey), idempotencyKey)
	require.Equal(t, "req_refund", result.Receipt.ProviderRequestID)
	require.Equal(t, "re_123", result.Receipt.ProviderObjectID)
	require.Equal(t, "pi_123", form.Get("payment_intent"))
	require.Equal(t, "2500", form.Get("amount"))
	require.Equal(t, "requested_by_customer", form.Get("reason"))
	require.Equal(t, "registration-123", form.Get("metadata[registration_id]"))
}

func TestCreateRefundProviderRejectionIncludesStripeCodeAndMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = response.Write([]byte(`{"error":{"type":"invalid_request_error","code":"charge_already_refunded","message":"Charge ch_123 has already been refunded."}}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunMutation(newStripeDexContext("refund-rejected"), client.CreateRefund(), stripeConnection, validRefundInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateRefundBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "charge_already_refunded")
	require.Contains(t, result.Failure.Message, "has already been refunded")
}

func TestCreateRefundRejectsInvalidInputAndResponses(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	for _, mutate := range []func(*stripe.CreateRefundInput){
		func(input *stripe.CreateRefundInput) { input.PaymentIntentID = "ch_123" },
		func(input *stripe.CreateRefundInput) { input.Amount = 0 },
		func(input *stripe.CreateRefundInput) { input.Reason = "because" },
	} {
		input := validRefundInput()
		mutate(&input)
		result, err := sdkgo.RunMutation(newStripeDexContext("refund-invalid"), client.CreateRefund(), stripeConnection, input)
		require.NoError(t, err)
		require.Equal(t, stripe.CreateRefundBranchDefect, result.Branch)
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"id":"re_123","object":"refund","status":"mystery","amount":2500,"currency":"usd"}`))
	}))
	defer server.Close()
	client = newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunMutation(newStripeDexContext("refund-malformed"), client.CreateRefund(), stripeConnection, validRefundInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateRefundBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

func validRefundInput() stripe.CreateRefundInput {
	return stripe.CreateRefundInput{
		PaymentIntentID: "pi_123", Amount: 2500, Reason: "requested_by_customer",
		Metadata: map[string]string{"registration_id": "registration-123"},
	}
}
