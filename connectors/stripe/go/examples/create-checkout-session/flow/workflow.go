// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createcheckoutsession demonstrates the Stripe create ACH Checkout
// Session Mutation in a Flow started from Dex Web Start Flow.
package createcheckoutsession

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "StripeCreateCheckoutSession"
	// ConnectionName is the static Dex Web connection for Stripe.
	ConnectionName = "stripe-payments"

	recordCheckoutRequestStepType = "RecordStripeCheckoutRequest"
	createCheckoutSessionStepType = "CreateStripeCheckoutSession"
	completeCheckoutStepType      = "CompleteStripeCheckoutSession"
)

var (
	checkoutRequestAttribute = dex.DefineAttribute[Input]("stripe-checkout-request")
	checkoutSessionAttribute = dex.DefineAttribute[stripe.CheckoutSession]("stripe-checkout-session")
)

// Input contains the Checkout Session fields entered in Dex Web Start Flow.
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

// Flow creates one hosted Stripe ACH Checkout Session.
type Flow struct {
	dex.FlowDefaults
	connection stripe.Connection
}

// NewFlow binds the Stripe Connection at registration time.
func NewFlow(connection stripe.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, connector, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCheckoutRequest{}),
		dex.DefineStep(stripe.NewCreateACHCheckoutSessionStep(stripe.CreateACHCheckoutSessionStepConfig[Input]{
			StepType: createCheckoutSessionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "stripe", GroupLabel: "Stripe",
				Explanation: "Create a hosted Stripe Checkout Session for the submitted ACH payment.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToCreateCheckoutSessionInput,
			Created: sdkgo.GoTo(completeCheckout{}),
		})),
		dex.DefineStep(completeCheckout{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and Checkout Session Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{checkoutRequestAttribute, checkoutSessionAttribute}}
}

// GetDexSummary returns the payment request and safe Checkout Session summary.
//
// dex:field attribute-key:stripe-checkout-request value-type:json editable:false description:"Submitted Checkout request"
// dex:field attribute-key:stripe-checkout-session value-type:json editable:false description:"Safe Checkout Session summary"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, session, err := checkoutInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"stripe-checkout-request": request,
		"stripe-checkout-session": session,
	}}, nil
}

// GetDexDisplay returns the payment request and hosted Checkout URL.
//
// dex:field attribute-key:stripe-checkout-request value-type:json editable:false description:"Amount, currency, product, and return URLs"
// dex:field attribute-key:stripe-checkout-session value-type:json editable:false description:"Session ID, hosted URL, payment status, and expiration"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, session, err := checkoutInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"stripe-checkout-request": request,
		"stripe-checkout-session": session,
	}}, nil
}

// MapToCreateCheckoutSessionInput maps the Flow input to the provider Mutation input.
func (*Flow) MapToCreateCheckoutSessionInput(input Input) stripe.CreateACHCheckoutSessionInput {
	return stripe.CreateACHCheckoutSessionInput{
		ClientReferenceID: input.ClientReferenceID,
		CustomerEmail:     input.CustomerEmail,
		SuccessURL:        input.SuccessURL,
		CancelURL:         input.CancelURL,
		Currency:          input.Currency,
		UnitAmount:        input.UnitAmount,
		ProductName:       input.ProductName,
	}
}

func checkoutInspection(ctx dex.Context) (Input, stripe.CheckoutSession, error) {
	request, err := optionalAttribute(ctx, checkoutRequestAttribute)
	if err != nil {
		return Input{}, stripe.CheckoutSession{}, err
	}
	session, err := optionalAttribute(ctx, checkoutSessionAttribute)
	if err != nil {
		return Input{}, stripe.CheckoutSession{}, err
	}
	return request, session, nil
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

// dex:group group-id:stripe group-label:"Stripe"
// dex:explanation text:"Validate and record the payment request before calling Stripe."
type recordCheckoutRequest struct {
	dex.StepDefaults
}

func (recordCheckoutRequest) GetStepType() string { return recordCheckoutRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordCheckoutRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCheckoutRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.ClientReferenceID = strings.TrimSpace(input.ClientReferenceID)
	input.CustomerEmail = strings.TrimSpace(input.CustomerEmail)
	input.SuccessURL = strings.TrimSpace(input.SuccessURL)
	input.CancelURL = strings.TrimSpace(input.CancelURL)
	input.Currency = strings.ToLower(strings.TrimSpace(input.Currency))
	input.ProductName = strings.TrimSpace(input.ProductName)
	if input.ClientReferenceID == "" || input.CustomerEmail == "" || input.SuccessURL == "" ||
		input.CancelURL == "" || input.Currency == "" || input.ProductName == "" || input.UnitAmount < 1 {
		return dex.ForceFail("all checkout fields are required and unitAmount must be positive"), nil
	}
	if err := checkoutRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](createCheckoutSessionStepType), input), nil
}

// dex:group group-id:stripe group-label:"Stripe"
// dex:explanation text:"Persist the safe Checkout Session summary and complete the Flow."
type completeCheckout struct {
	dex.StepDefaultsNoWaitFor[stripe.CreateACHCheckoutSessionResult]
}

func (completeCheckout) GetStepType() string { return completeCheckoutStepType }

func (completeCheckout) Execute(ctx dex.Context, result stripe.CreateACHCheckoutSessionResult) (*dex.StepDecision, error) {
	if err := checkoutSessionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
