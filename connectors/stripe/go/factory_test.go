// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/stripe/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type stripeMutationTarget struct {
	dex.StepDefaultsNoWaitFor[stripe.CreateACHCheckoutSessionResult]
}

func (stripeMutationTarget) Execute(dex.Context, stripe.CreateACHCheckoutSessionResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type stripeQueryTarget struct {
	dex.StepDefaultsNoWaitFor[stripe.GetCheckoutSessionResult]
}

func (stripeQueryTarget) Execute(dex.Context, stripe.GetCheckoutSessionResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestStripeFactoriesRequireHappyPathsAndAllowOptionalBranches(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	require.NotPanics(t, func() {
		stripe.NewCreateACHCheckoutSessionStep(stripe.CreateACHCheckoutSessionStepConfig[string]{
			StepType: "CreateCheckout", Annotations: sdkgo.StepAnnotations{GroupID: "stripe", GroupLabel: "Stripe", Explanation: "Create Checkout."},
			Connection: connection, ConnectionName: "payments",
			MapToOperationInput: func(string) stripe.CreateACHCheckoutSessionInput { return validCreateInput() },
			Created:             sdkgo.GoTo(stripeMutationTarget{}),
		})
		stripe.NewGetCheckoutSessionStep(stripe.GetCheckoutSessionStepConfig[string]{
			StepType: "GetCheckout", Annotations: sdkgo.StepAnnotations{GroupID: "stripe", GroupLabel: "Stripe", Explanation: "Get Checkout."},
			Connection: connection, ConnectionName: "payments",
			MapToOperationInput: func(string) stripe.GetCheckoutSessionInput {
				return stripe.GetCheckoutSessionInput{SessionID: "cs_test_123"}
			},
			Found: sdkgo.GoTo(stripeQueryTarget{}),
		})
	})
	require.Panics(t, func() {
		stripe.NewCreateACHCheckoutSessionStep(stripe.CreateACHCheckoutSessionStepConfig[string]{
			StepType: "CreateCheckout", Annotations: sdkgo.StepAnnotations{GroupID: "stripe", GroupLabel: "Stripe", Explanation: "Create Checkout."},
			Connection: connection, ConnectionName: "payments",
			MapToOperationInput: func(string) stripe.CreateACHCheckoutSessionInput { return validCreateInput() },
			ProviderRejected:    sdkgo.GoTo(stripeMutationTarget{}),
		})
	})
}

func TestStripeConnectionCannotBeSerialized(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	_, err = json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "sk_test_example")
	require.NotContains(t, err.Error(), "whsec_example")
}

type stripePaymentIntentTarget struct {
	dex.StepDefaultsNoWaitFor[stripe.GetPaymentIntentResult]
}

func (stripePaymentIntentTarget) Execute(dex.Context, stripe.GetPaymentIntentResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

type stripeRefundTarget struct {
	dex.StepDefaultsNoWaitFor[stripe.CreateRefundResult]
}

func (stripeRefundTarget) Execute(dex.Context, stripe.CreateRefundResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestStripePaymentFactoriesRequireHappyPathsAndAllowOptionalBranches(t *testing.T) {
	client := newStripeClient(t, "http://127.0.0.1:1", stripe.Config{})
	connection, err := stripe.NewConnection(client, stripeConnection)
	require.NoError(t, err)
	annotations := sdkgo.StepAnnotations{GroupID: "stripe", GroupLabel: "Stripe", Explanation: "Stripe payment step."}
	require.NotPanics(t, func() {
		stripe.NewGetPaymentIntentStep(stripe.GetPaymentIntentStepConfig[string]{
			StepType: "GetIntent", Annotations: annotations, Connection: connection, ConnectionName: "payments",
			MapToOperationInput: func(string) stripe.GetPaymentIntentInput {
				return stripe.GetPaymentIntentInput{PaymentIntentID: "pi_123"}
			},
			Found: sdkgo.GoTo(stripePaymentIntentTarget{}),
		})
		stripe.NewCreateRefundStep(stripe.CreateRefundStepConfig[string]{
			StepType: "CreateRefund", Annotations: annotations, Connection: connection, ConnectionName: "payments",
			MapToOperationInput: func(string) stripe.CreateRefundInput { return validRefundInput() },
			Refunded:            sdkgo.GoTo(stripeRefundTarget{}),
		})
	})
	require.Panics(t, func() {
		stripe.NewCreateRefundStep(stripe.CreateRefundStepConfig[string]{
			StepType: "CreateRefund", Annotations: annotations, Connection: connection, ConnectionName: "payments",
			MapToOperationInput: func(string) stripe.CreateRefundInput { return validRefundInput() },
			ProviderRejected:    sdkgo.GoTo(stripeRefundTarget{}),
		})
	})
}
