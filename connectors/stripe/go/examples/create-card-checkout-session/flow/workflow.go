// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createcardcheckoutsession demonstrates hosted Stripe card Checkout.
package createcardcheckoutsession

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ConnectionName is the static Dex Web connection for Stripe.
const ConnectionName = "stripe-payments"

// Input contains the card Checkout fields entered in Dex Web Start Flow.
type Input struct {
	// ClientReferenceID is the application's non-sensitive payment reference.
	ClientReferenceID string `json:"clientReferenceId"`
	// CustomerEmail is the plain email address pre-filled by Stripe Checkout.
	CustomerEmail string `json:"customerEmail"`
	// SuccessURL is the HTTPS return location after successful Checkout.
	SuccessURL string `json:"successUrl"`
	// CancelURL is the HTTPS return location after cancelled Checkout.
	CancelURL string `json:"cancelUrl"`
	// Currency is the lowercase three-letter settlement currency.
	Currency string `json:"currency"`
	// UnitAmount is the positive price in the currency's minor unit.
	UnitAmount int64 `json:"unitAmount"`
	// ProductName is the customer-visible product or event name.
	ProductName string `json:"productName"`
}

var (
	cardCheckoutRequest = dex.DefineAttribute[Input]("card-checkout-request")
	cardCheckoutSession = dex.DefineAttribute[stripe.CheckoutSession]("card-checkout-session")
)

// CardFlow creates a hosted Stripe card Checkout Session.
type CardFlow struct {
	dex.FlowDefaults
	connection stripe.Connection
}

// NewCardFlow binds the Stripe connection at Worker registration.
func NewCardFlow(connection stripe.Connection) *CardFlow {
	return &CardFlow{connection: connection}
}

// GetSteps defines card Checkout creation and safe result recording.
func (flow *CardFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(PrepareCardCheckout{}),
		dex.DefineStep(stripe.NewCreateCardCheckoutSessionStep(stripe.CreateCardCheckoutSessionStepConfig[Input]{
			StepType: "CreateCardCheckout", Connection: flow.connection, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "card-checkout", GroupLabel: "Card Checkout", Explanation: "Create one hosted card Checkout Session."},
			MapToOperationInput: func(input Input) stripe.CreateCardCheckoutSessionInput {
				return stripe.CreateCardCheckoutSessionInput{
					ClientReferenceID: input.ClientReferenceID,
					CustomerEmail:     input.CustomerEmail,
					SuccessURL:        input.SuccessURL,
					CancelURL:         input.CancelURL,
					Currency:          input.Currency,
					UnitAmount:        input.UnitAmount,
					ProductName:       input.ProductName,
				}
			},
			Created: sdkgo.GoTo(CompleteCardCheckout{}),
		})),
		dex.DefineStep(CompleteCardCheckout{}),
	}
}

// GetRPCs exposes the example's safe Checkout summary and display.
func (flow *CardFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the card request and safe Checkout summary.
func (*CardFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{cardCheckoutRequest, cardCheckoutSession}}
}

// dex:field attribute-key:card-checkout-request value-type:json editable:false description:"Card Checkout request"
// dex:field attribute-key:card-checkout-session value-type:json editable:false description:"Hosted card Checkout summary"
func (*CardFlow) GetDexSummary(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[map[string]any], err error) {
	request, session, err := cardInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"card-checkout-request": request,
		"card-checkout-session": session,
	}}, nil
}

// dex:field attribute-key:card-checkout-request value-type:json editable:false description:"Card Checkout request"
// dex:field attribute-key:card-checkout-session value-type:json editable:false description:"Hosted card Checkout summary"
func (*CardFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[map[string]any], err error) {
	request, session, err := cardInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"card-checkout-request": request,
		"card-checkout-session": session,
	}}, nil
}

func cardInspection(ctx dex.Context) (Input, stripe.CheckoutSession, error) {
	request, err := optionalAttribute(ctx, cardCheckoutRequest)
	if err != nil {
		return Input{}, stripe.CheckoutSession{}, err
	}
	session, err := optionalAttribute(ctx, cardCheckoutSession)
	if err != nil {
		return Input{}, stripe.CheckoutSession{}, err
	}
	return request, session, nil
}

// dex:group group-id:card-checkout group-label:"Card Checkout"
// dex:explanation text:"Validate and retain the card Checkout request."
type PrepareCardCheckout struct {
	dex.StepDefaultsNoWaitFor[Input]
}

// Execute retains the request before calling Stripe.
func (PrepareCardCheckout) Execute(ctx dex.Context, input Input) (_ *dex.StepDecision, err error) {
	input.ClientReferenceID = strings.TrimSpace(input.ClientReferenceID)
	input.CustomerEmail = strings.TrimSpace(input.CustomerEmail)
	input.SuccessURL = strings.TrimSpace(input.SuccessURL)
	input.CancelURL = strings.TrimSpace(input.CancelURL)
	input.Currency = strings.ToLower(strings.TrimSpace(input.Currency))
	input.ProductName = strings.TrimSpace(input.ProductName)
	if input.ClientReferenceID == "" || input.CustomerEmail == "" || input.SuccessURL == "" ||
		input.CancelURL == "" || input.Currency == "" || input.ProductName == "" || input.UnitAmount < 1 {
		return dex.ForceFail("all card Checkout fields are required and unitAmount must be positive"), nil
	}
	if err = cardCheckoutRequest.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input]("CreateCardCheckout"), input), nil
}

// dex:group group-id:card-checkout group-label:"Card Checkout"
// dex:explanation text:"Retain the safe card Checkout result."
type CompleteCardCheckout struct {
	dex.StepDefaultsNoWaitFor[stripe.CreateCardCheckoutSessionResult]
}

// Execute retains the created session and completes the example.
func (CompleteCardCheckout) Execute(ctx dex.Context, result stripe.CreateCardCheckoutSessionResult) (_ *dex.StepDecision, err error) {
	err = cardCheckoutSession.Set(ctx, result.Value)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}
