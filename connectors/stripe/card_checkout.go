// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"errors"
	"net/http"
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
	if err := input.validate(); err != nil {
		return sdkgo.NewMutationBranch(CreateCardCheckoutSessionBranchDefect, CheckoutSession{}, stripeFailurePointer("createCardCheckoutSession", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "createCardCheckoutSession")
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateCardCheckoutSessionBranchDefect, CheckoutSession{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.request(call, credentials.SecretKey.Reveal(), http.MethodPost, "checkout/sessions", input.formValues())
	if err != nil {
		if errors.Is(err, errStripeRequestInvalid) {
			return sdkgo.NewMutationBranch(CreateCardCheckoutSessionBranchDefect, CheckoutSession{}, stripeFailurePointer("createCardCheckoutSession", sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		if errors.Is(err, errStripeResponseTooLarge) {
			return sdkgo.NewMutationRetry[CheckoutSession](stripeFailure("createCardCheckoutSession", sdkgo.FailureResponseTooLarge, err.Error()), retryAfter(response.header))
		}
		return sdkgo.NewMutationRetry[CheckoutSession](stripeFailure("createCardCheckoutSession", sdkgo.FailureTransport, "Stripe Checkout Session response is unavailable"), retryAfter(response.header))
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CheckoutSession](stripeFailure("createCardCheckoutSession", sdkgo.FailureRateLimit, "Stripe rate limited the Checkout Session request"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewMutationRetry[CheckoutSession](stripeFailure("createCardCheckoutSession", sdkgo.FailureAvailability, "Stripe Checkout Session is temporarily unavailable"), retryAfter(response.header))
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		return sdkgo.NewMutationBranch(CreateCardCheckoutSessionBranchProviderRejected, CheckoutSession{}, classifyStripeRejection("createCardCheckoutSession", response), operation.client.receipt(call, response, ""))
	}
	session, err := decodeCheckoutSession(response.body)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateCardCheckoutSessionBranchInvalidResponse, CheckoutSession{}, stripeFailurePointer("createCardCheckoutSession", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, response, ""))
	}
	return sdkgo.NewMutationBranch(CreateCardCheckoutSessionBranchCreated, session, nil, operation.client.receipt(call, response, session.ID))
}
