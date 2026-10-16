// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestStripeAPIKeyOnlyAcceptsOperationsWithoutWebhookSecret(t *testing.T) {
	credentials := stripe.Credentials{
		AuthMethodID: "api-key-only",
		SecretKey:    sdkgo.NewSecretString("rk_test_example"),
	}
	require.NoError(t, credentials.Validate())

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/payment_intents/pi_123":
			_, _ = response.Write([]byte(`{"id":"pi_123","object":"payment_intent","amount":1000,"currency":"usd","status":"succeeded","latest_charge":null}`))
		case request.Method == http.MethodPost && request.URL.Path == "/refunds":
			_, _ = response.Write([]byte(`{"id":"re_123","object":"refund","status":"succeeded","amount":500,"currency":"usd"}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := stripe.New(
		stripe.Config{Endpoint: server.URL},
		sdkgo.StaticCredentialProvider[stripe.Credentials]{stripeConnection: credentials},
		stripe.WithClock(func() time.Time { return time.Unix(1_700_000_000, 0) }),
	)
	require.NoError(t, err)
	paymentIntent, err := sdkgo.RunQuery(newStripeDexContext("api-key-only-lookup"), client.GetPaymentIntent(), stripeConnection,
		stripe.GetPaymentIntentInput{PaymentIntentID: "pi_123"})
	require.NoError(t, err)
	require.Equal(t, stripe.GetPaymentIntentBranchFound, paymentIntent.Branch)
	refund, err := sdkgo.RunMutation(newStripeDexContext("api-key-only-refund"), client.CreateRefund(), stripeConnection,
		stripe.CreateRefundInput{PaymentIntentID: "pi_123", Amount: 500, Reason: "requested_by_customer"})
	require.NoError(t, err)
	require.Equal(t, stripe.CreateRefundBranchRefunded, refund.Branch)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)
	body := `{"id":"evt_123","object":"event","type":"checkout.session.completed","created":1700000000,"data":{"object":{"id":"cs_test_123","object":"checkout.session"}}}`
	require.Equal(t, http.StatusServiceUnavailable, serveSignedWebhook(handler, body, "whsec_example", time.Unix(1_700_000_000, 0)))
}

func TestStripeWebhookMethodStillRequiresSigningSecret(t *testing.T) {
	apiKey := sdkgo.NewSecretString("rk_test_example")
	require.ErrorContains(t, (stripe.Credentials{
		AuthMethodID: "api-key-with-webhook",
		SecretKey:    apiKey,
	}).Validate(), "webhook_secret")
	require.NoError(t, (stripe.Credentials{
		AuthMethodID:  "api-key-with-webhook",
		SecretKey:     apiKey,
		WebhookSecret: sdkgo.NewSecretString("whsec_example"),
	}).Validate())
	require.ErrorContains(t, (stripe.Credentials{
		AuthMethodID: "api-key-only",
	}).Validate(), "secret_key")
	require.ErrorContains(t, (stripe.Credentials{
		AuthMethodID: "unknown",
		SecretKey:    apiKey,
	}).Validate(), "auth_method")
}
