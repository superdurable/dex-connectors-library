// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateCardCheckoutSessionInput is one hosted card Checkout request.
// Its fields have the same meanings and validation as CreateACHCheckoutSessionInput.
type CreateCardCheckoutSessionInput CreateACHCheckoutSessionInput

func (input CreateCardCheckoutSessionInput) validate() error {
	return CreateACHCheckoutSessionInput(input).validate()
}

func (input CreateCardCheckoutSessionInput) formValues() url.Values {
	values := CreateACHCheckoutSessionInput(input).formValues()
	values.Set("payment_method_types[0]", "card")
	return values
}

// CreateCardCheckoutSessionOperation creates a Stripe-hosted card Checkout Session.
type CreateCardCheckoutSessionOperation struct{ client *Client }

// CreateCardCheckoutSession returns the card Checkout mutation bound to this client.
func (client *Client) CreateCardCheckoutSession() CreateCardCheckoutSessionOperation {
	return CreateCardCheckoutSessionOperation{client: client}
}

// Definition returns the immutable card Checkout operation definition.
func (CreateCardCheckoutSessionOperation) Definition() sdkgo.MutationDefinition {
	return CreateCardCheckoutSessionDefinition
}

// IdempotencyKey maps the stable connector call ID to Stripe's provider key.
func (CreateCardCheckoutSessionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCardCheckoutSessionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one hosted Checkout Session that accepts card payments.
func (operation CreateCardCheckoutSessionOperation) Invoke(call sdkgo.Call, input CreateCardCheckoutSessionInput) sdkgo.MutationAttempt[CheckoutSession] {
	return operation.client.createCheckoutSession(call, checkoutSessionWrite{
		operation: "createCardCheckoutSession", created: CreateCardCheckoutSessionBranchCreated,
		providerRejected: CreateCardCheckoutSessionBranchProviderRejected, invalidResponse: CreateCardCheckoutSessionBranchInvalidResponse,
		defect: CreateCardCheckoutSessionBranchDefect,
	}, input.validate(), input.formValues)
}
