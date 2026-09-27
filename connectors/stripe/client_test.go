// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var stripeConnection = sdkgo.ConnectionRef{Provider: "stripe", Name: "payments"}

func TestCreateACHCheckoutSessionUsesHostedBankPaymentAndStableIdempotency(t *testing.T) {
	var form url.Values
	var idempotencyKey string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/checkout/sessions", request.URL.Path)
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("sk_test_example:")), request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		form = request.Form
		idempotencyKey = request.Header.Get("Idempotency-Key")
		require.NotEmpty(t, idempotencyKey)
		response.Header().Set("Request-Id", "req_create")
		_, _ = response.Write([]byte(`{"id":"cs_test_123","object":"checkout.session","url":"https://checkout.stripe.com/c/pay/cs_test_123","client_reference_id":"registration-123","payment_status":"unpaid","status":"open","currency":"usd","amount_total":7500,"expires_at":1700000000,"metadata":{"registration_id":"registration-123"}}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunMutation(newStripeDexContext("create-checkout"), client.CreateACHCheckoutSession(), stripeConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateACHCheckoutSessionBranchCreated, result.Branch)
	require.Equal(t, "cs_test_123", result.Value.ID)
	require.Equal(t, "req_create", result.Receipt.ProviderRequestID)
	require.Equal(t, string(result.Receipt.IdempotencyKey), idempotencyKey)
	require.Equal(t, "payment", form.Get("mode"))
	require.Equal(t, "us_bank_account", form.Get("payment_method_types[0]"))
	require.Equal(t, "7500", form.Get("line_items[0][price_data][unit_amount]"))
	require.Equal(t, "registration-123", form.Get("metadata[registration_id]"))
	require.Equal(t, "registration-123", form.Get("payment_intent_data[metadata][registration_id]"))
	require.Empty(t, form.Get("secret_key"))
}

func TestGetCheckoutSessionReturnsSafeNormalizedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/checkout/sessions/cs_test_123", request.URL.Path)
		_, _ = response.Write([]byte(`{"id":"cs_test_123","object":"checkout.session","payment_status":"paid","status":"complete","payment_intent":{"id":"pi_123","object":"payment_intent"},"currency":"usd","amount_total":7500}`))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunQuery(newStripeDexContext("get-checkout"), client.GetCheckoutSession(), stripeConnection, stripe.GetCheckoutSessionInput{SessionID: "cs_test_123"})
	require.NoError(t, err)
	require.Equal(t, stripe.GetCheckoutSessionBranchFound, result.Branch)
	require.Equal(t, "paid", result.Value.PaymentStatus)
	require.Equal(t, "pi_123", result.Value.PaymentIntentID)
}

func TestGetCheckoutSessionClassifiesNotFoundAndAuthentication(t *testing.T) {
	statuses := []struct {
		status  int
		branch  sdkgo.BranchID
		failure sdkgo.FailureKind
	}{
		{http.StatusNotFound, stripe.GetCheckoutSessionBranchNotFound, sdkgo.FailureNotFound},
		{http.StatusUnauthorized, stripe.GetCheckoutSessionBranchProviderRejected, sdkgo.FailureAuthentication},
	}
	for _, testCase := range statuses {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(testCase.status)
			_, _ = response.Write([]byte(`{"error":{"type":"invalid_request_error"}}`))
		}))
		client := newStripeClient(t, server.URL, stripe.Config{})
		result, err := sdkgo.RunQuery(newStripeDexContext("get-error"), client.GetCheckoutSession(), stripeConnection, stripe.GetCheckoutSessionInput{SessionID: "cs_missing"})
		require.NoError(t, err)
		require.Equal(t, testCase.branch, result.Branch)
		require.Equal(t, testCase.failure, result.Failure.Kind)
		server.Close()
	}
}

func TestCreateACHCheckoutSessionClassifiesValidationAndAmbiguousFailure(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	invalid := validCreateInput()
	invalid.Currency = "USD"
	result, err := sdkgo.RunMutation(newStripeDexContext("invalid-checkout"), client.CreateACHCheckoutSession(), stripeConnection, invalid)
	require.NoError(t, err)
	require.Equal(t, stripe.CreateACHCheckoutSessionBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)

	result, err = sdkgo.RunMutation(newStripeDexContext("ambiguous-checkout"), client.CreateACHCheckoutSession(), stripeConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateACHCheckoutSessionBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
}

func TestCreateACHCheckoutSessionRejectsOversizedProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(strings.Repeat("x", 65)))
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{MaxResponseBytes: 64})
	result, err := sdkgo.RunMutation(newStripeDexContext("oversized-checkout"), client.CreateACHCheckoutSession(), stripeConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateACHCheckoutSessionBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestCreateACHCheckoutSessionDoesNotFollowRedirectsWithCredentials(t *testing.T) {
	receivedRedirect := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { receivedRedirect = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", destination.URL)
		response.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := newStripeClient(t, server.URL, stripe.Config{})
	result, err := sdkgo.RunMutation(newStripeDexContext("redirect-checkout"), client.CreateACHCheckoutSession(), stripeConnection, validCreateInput())
	require.NoError(t, err)
	require.Equal(t, stripe.CreateACHCheckoutSessionBranchProviderRejected, result.Branch)
	require.False(t, receivedRedirect)
}

func validCreateInput() stripe.CreateACHCheckoutSessionInput {
	return stripe.CreateACHCheckoutSessionInput{
		ClientReferenceID: "registration-123", CustomerEmail: "attendee@example.com",
		SuccessURL: "https://events.example.com/registration/registration-123?session_id={CHECKOUT_SESSION_ID}",
		CancelURL:  "https://events.example.com/register", Currency: "usd", UnitAmount: 7500,
		ProductName: "Community Event", Metadata: map[string]string{"registration_id": "registration-123"},
	}
}

func newStripeClient(t *testing.T, endpoint string, config stripe.Config) *stripe.Client {
	t.Helper()
	config.Endpoint = endpoint
	client, err := stripe.New(config, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		stripeConnection: {SecretKey: sdkgo.NewSecretString("sk_test_example"), WebhookSecret: sdkgo.NewSecretString("whsec_example")},
	})
	require.NoError(t, err)
	return client
}

type stripeDexContext struct {
	context.Context
	step string
}

func newStripeDexContext(step string) *stripeDexContext {
	return &stripeDexContext{Context: context.Background(), step: step}
}

func (*stripeDexContext) FlowID() string                                  { return "registration-123" }
func (*stripeDexContext) RunID() string                                   { return "run" }
func (*stripeDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *stripeDexContext) StepExecutionID() string                 { return context.step }
func (*stripeDexContext) FromStepExecutionID() string                     { return "" }
func (*stripeDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*stripeDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*stripeDexContext) Attempt() int32                                  { return 1 }
func (*stripeDexContext) HasTimerFired() bool                             { return false }
func (*stripeDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*stripeDexContext) WaitForMethodFailed() bool                       { return false }
func (*stripeDexContext) RecordHeartbeat(any) error                       { return nil }
func (*stripeDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*stripeDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*stripeDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*stripeDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*stripeDexContext)(nil)
