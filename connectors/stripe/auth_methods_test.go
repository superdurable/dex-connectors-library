// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestStripeAPIKeyOnlyAcceptsOperationsWithoutWebhookSecret(t *testing.T) {
	credentials := stripe.Credentials{
		AuthMethodID: "api-key-only",
		SecretKey: sdkgo.NewSecretString("rk_test_example"),
	}
	require.NoError(t, credentials.Validate())

	client, err := stripe.New(
		stripe.Config{Endpoint: "http://127.0.0.1:1"},
		sdkgo.StaticCredentialProvider[stripe.Credentials]{stripeConnection: credentials},
		stripe.WithClock(func() time.Time { return time.Unix(1_700_000_000, 0) }),
	)
	require.NoError(t, err)
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
		SecretKey: apiKey,
	}).Validate(), "webhook_secret")
	require.NoError(t, (stripe.Credentials{
		AuthMethodID: "api-key-with-webhook",
		SecretKey: apiKey,
		WebhookSecret: sdkgo.NewSecretString("whsec_example"),
	}).Validate())
	require.ErrorContains(t, (stripe.Credentials{
		AuthMethodID: "api-key-only",
	}).Validate(), "secret_key")
	require.ErrorContains(t, (stripe.Credentials{
		AuthMethodID: "unknown",
		SecretKey: apiKey,
	}).Validate(), "auth_method")
}
