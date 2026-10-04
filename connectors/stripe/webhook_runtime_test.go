// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// stripeProjectConfiguration is the project configuration Dex Web saves for the connection and its bindings.
func stripeProjectConfiguration(bindings map[string]string) projectconfig.Configuration {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: stripe.ConnectorID, ConnectionName: stripeConnection.Name,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/stripe", Provider: "stripe",
		Configuration: json.RawMessage(`{}`),
	}}}
	for bindingName, bindingConfiguration := range bindings {
		configuration.TriggerBindings = append(configuration.TriggerBindings, projectconfig.TriggerConfiguration{
			ConnectorID: stripe.ConnectorID, ConnectionName: stripeConnection.Name, TriggerName: "checkoutSessionUpdated",
			BindingName: bindingName, Configuration: json.RawMessage(bindingConfiguration),
		})
	}
	return configuration
}

// recordingCheckoutInboxes stands in for durable project inboxes, recording each inbox key and binding target.
type recordingCheckoutInboxes struct {
	keys    []projectconfig.TriggerInboxKey
	targets []*recordingCheckoutTarget
}

func (inboxes *recordingCheckoutInboxes) wrap(
	key projectconfig.TriggerInboxKey, _ sdkgo.TriggerTarget[stripe.CheckoutSessionEvent],
) (sdkgo.TriggerTarget[stripe.CheckoutSessionEvent], error) {
	target := &recordingCheckoutTarget{}
	inboxes.keys = append(inboxes.keys, key)
	inboxes.targets = append(inboxes.targets, target)
	return target, nil
}

func TestCheckoutSessionWebhookRuntimeRecordsEachBindingBeforeAcknowledging(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client, err := stripe.New(stripe.Config{Endpoint: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[stripe.Credentials]{
		stripeConnection: {SecretKey: sdkgo.NewSecretString("sk_test_example"), WebhookSecret: sdkgo.NewSecretString("whsec_example")},
	}, stripe.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	handler, err := connection.CheckoutSessionWebhookHandler()
	require.NoError(t, err)
	endpoint, hasWebhookEndpoint := handler.(*webhooktrigger.Endpoint[stripe.Credentials, stripe.CheckoutSessionEvent])
	require.True(t, hasWebhookEndpoint)
	unusedTarget := sdkgo.TriggerTargetFunc[stripe.CheckoutSessionEvent](func(context.Context, sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
		return nil
	})
	inboxes := &recordingCheckoutInboxes{}
	runtime, err := stripe.NewCheckoutSessionWebhookRuntimeForTest(connection, stripeProjectConfiguration(map[string]string{
		"registration-payments": `{"eventTypes":["checkout.session.async_payment_succeeded"]}`,
		"expirations":           `{"eventTypes":["checkout.session.expired"]}`,
	}), []stripe.ProjectCheckoutSessionUpdatedTriggerRoute{
		{BindingName: "registration-payments", Target: unusedTarget},
		{BindingName: "expirations", Target: unusedTarget},
	}, inboxes.wrap)
	require.NoError(t, err)
	require.Equal(t, []projectconfig.TriggerInboxKey{
		{ConnectorID: stripe.ConnectorID, ConnectionName: stripeConnection.Name, TriggerName: "checkoutSessionUpdated", BindingName: "registration-payments"},
		{ConnectorID: stripe.ConnectorID, ConnectionName: stripeConnection.Name, TriggerName: "checkoutSessionUpdated", BindingName: "expirations"},
	}, inboxes.keys, "every route's target is wrapped in its binding's durable inbox")

	body := `{"id":"evt_paid","object":"event","type":"checkout.session.async_payment_succeeded","created":1700000000,"data":{"object":{"id":"cs_test_123","object":"checkout.session","client_reference_id":"registration-123","payment_status":"paid","status":"complete","payment_intent":"pi_123","currency":"usd","amount_total":7500}}}`
	require.Equal(t, http.StatusServiceUnavailable, serveSignedWebhook(runtime, body, "whsec_example", now), "no binding runs yet")
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runtime.Run(ctx) }()
	defer func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	}()
	// A filtering binding can acknowledge before the matching binding starts.
	require.Eventually(t, func() bool {
		return endpoint.RunningSourceCount() == len(inboxes.targets)
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, http.StatusOK, serveSignedWebhook(runtime, body, "whsec_example", now))
	require.Eventually(t, func() bool { return len(inboxes.targets[0].calls()) == 2 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"prepare:evt_paid", "handle:evt_paid"}, inboxes.targets[0].calls())
	require.Empty(t, inboxes.targets[1].calls(), "the expirations binding filters a payment event")
}

func TestCheckoutSessionWebhookRuntimeRejectsIncompleteRoutes(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	target := sdkgo.TriggerTargetFunc[stripe.CheckoutSessionEvent](func(context.Context, sdkgo.TriggerEvent[stripe.CheckoutSessionEvent]) error {
		return nil
	})
	configuration := stripeProjectConfiguration(map[string]string{
		"registration-payments": `{}`,
		"invalid":               `{"eventTypes":["charge.succeeded"]}`,
		"unknown":               `{"events":["checkout.session.completed"]}`,
	})
	inboxes := &recordingCheckoutInboxes{}
	_, err = stripe.NewCheckoutSessionWebhookRuntimeForTest(connection, configuration, []stripe.ProjectCheckoutSessionUpdatedTriggerRoute{
		{BindingName: "registration-payments", Target: target},
	}, inboxes.wrap)
	require.NoError(t, err)
	for name, routes := range map[string][]stripe.ProjectCheckoutSessionUpdatedTriggerRoute{
		"no routes":          nil,
		"unstored binding":   {{BindingName: "missing", Target: target}},
		"invalid binding":    {{BindingName: "invalid", Target: target}},
		"unknown member":     {{BindingName: "unknown", Target: target}},
		"blank binding name": {{BindingName: " ", Target: target}},
		"nil target":         {{BindingName: "registration-payments"}},
		"duplicated binding": {{BindingName: "registration-payments", Target: target}, {BindingName: "registration-payments", Target: target}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := stripe.NewCheckoutSessionWebhookRuntimeForTest(connection, configuration, routes, inboxes.wrap)
			require.Error(t, err)
		})
	}
	_, err = stripe.NewCheckoutSessionWebhookRuntimeForTest(connection, configuration, []stripe.ProjectCheckoutSessionUpdatedTriggerRoute{
		{BindingName: "missing", Target: target},
	}, inboxes.wrap)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	inboxFailure := errors.New("project trigger inbox is unavailable")
	_, err = stripe.NewCheckoutSessionWebhookRuntimeForTest(connection, configuration, []stripe.ProjectCheckoutSessionUpdatedTriggerRoute{
		{BindingName: "registration-payments", Target: target},
	}, func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[stripe.CheckoutSessionEvent]) (sdkgo.TriggerTarget[stripe.CheckoutSessionEvent], error) {
		return nil, inboxFailure
	})
	require.ErrorIs(t, err, inboxFailure)
	_, err = stripe.NewProjectCheckoutSessionWebhookRuntime(nil, stripeConnection.Name, []stripe.ProjectCheckoutSessionUpdatedTriggerRoute{
		{BindingName: "registration-payments", Target: target},
	})
	require.Error(t, err)
}
