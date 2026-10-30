// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package refundpayment demonstrates the Stripe getPaymentIntent Query and
// createRefund Mutation in a Flow started from Dex Web Start Flow.
package refundpayment

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/stripe"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "StripeRefundPayment"
	// ConnectionName is the static Dex Web connection for Stripe.
	ConnectionName = "stripe-payments"

	recordRefundRequestStepType = "RecordStripeRefundRequest"
	getPaymentIntentStepType    = "GetStripePaymentIntent"
	planRefundStepType          = "PlanStripeRefund"
	createRefundStepType        = "CreateStripeRefund"
	completeRefundStepType      = "CompleteStripeRefund"
)

var (
	refundRequestAttribute = dex.DefineAttribute[Input]("stripe-refund-request")
	refundOutcomeAttribute = dex.DefineAttribute[Outcome]("stripe-refund-outcome")
)

// Input names the payment to refund in full for what remains unrefunded.
type Input struct {
	// PaymentIntentID is the Stripe PaymentIntent, beginning with pi_.
	PaymentIntentID string `json:"paymentIntentId"`
	// Reason optionally is duplicate, fraudulent, or requested_by_customer.
	Reason string `json:"reason,omitempty"`
}

// Outcome is the safe summary of the refund decision.
type Outcome struct {
	// PaymentIntent is the PaymentIntent read before refunding.
	PaymentIntent stripe.PaymentIntent `json:"paymentIntent"`
	// Refund is Stripe's refund; it is empty when nothing remained to refund.
	Refund stripe.Refund `json:"refund"`
	// IsRefundCreated reports whether this Flow asked Stripe for a refund.
	IsRefundCreated bool `json:"isRefundCreated"`
}

// RefundRequest is the planned refund of the remaining refundable balance.
type RefundRequest struct {
	// PaymentIntent is the PaymentIntent that was read.
	PaymentIntent stripe.PaymentIntent `json:"paymentIntent"`
	// Reason is the refund reason entered at start.
	Reason string `json:"reason,omitempty"`
}

// Flow reads one PaymentIntent and refunds what remains unrefunded.
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

// GetSteps returns the request, Stripe, planning, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRefundRequest{}),
		dex.DefineStep(stripe.NewGetPaymentIntentStep(stripe.GetPaymentIntentStepConfig[Input]{
			StepType: getPaymentIntentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "stripe-lookup", GroupLabel: "Stripe lookup", Explanation: "Read the PaymentIntent to learn how much of it is still refundable."},
			Connection:  flow.connection, MapToOperationInput: MapToGetPaymentIntentInput,
			Found: sdkgo.GoTo(planRefund{}),
		})),
		dex.DefineStep(planRefund{}),
		dex.DefineStep(stripe.NewCreateRefundStep(stripe.CreateRefundStepConfig[RefundRequest]{
			StepType: createRefundStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "stripe-payment", GroupLabel: "Stripe payment", Explanation: "Refund the remaining balance with a Stripe idempotency key derived from the Step."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateRefundInput,
			Refunded: sdkgo.GoTo(completeRefund{}),
		})),
		dex.DefineStep(completeRefund{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{refundRequestAttribute, refundOutcomeAttribute}}
}

// GetDexSummary returns the refund request and its safe outcome.
//
// dex:field attribute-key:stripe-refund-request value-type:json editable:false description:"Submitted refund request"
// dex:field attribute-key:stripe-refund-outcome value-type:json editable:false description:"PaymentIntent and refund summary"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"stripe-refund-request": request,
		"stripe-refund-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the refund request and its safe outcome.
//
// dex:field attribute-key:stripe-refund-request value-type:json editable:false description:"PaymentIntent and reason"
// dex:field attribute-key:stripe-refund-outcome value-type:json editable:false description:"Refund ID, status, amount, and currency"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := refundInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"stripe-refund-request": request,
		"stripe-refund-outcome": outcome,
	}}, nil
}

// MapToGetPaymentIntentInput maps the Flow input to the PaymentIntent Query input.
func MapToGetPaymentIntentInput(input Input) stripe.GetPaymentIntentInput {
	return stripe.GetPaymentIntentInput{PaymentIntentID: input.PaymentIntentID}
}

// MapToCreateRefundInput refunds everything that the PaymentIntent has not yet refunded.
func MapToCreateRefundInput(request RefundRequest) stripe.CreateRefundInput {
	return stripe.CreateRefundInput{
		PaymentIntentID: request.PaymentIntent.ID,
		Amount:          RefundableAmount(request.PaymentIntent),
		Reason:          request.Reason,
	}
}

// RefundableAmount returns the minor-unit amount of a succeeded PaymentIntent that is not yet refunded.
func RefundableAmount(paymentIntent stripe.PaymentIntent) int64 {
	if paymentIntent.Status != "succeeded" || paymentIntent.AmountRefunded >= paymentIntent.Amount {
		return 0
	}
	return paymentIntent.Amount - paymentIntent.AmountRefunded
}

func refundInspection(ctx dex.Context) (Input, Outcome, error) {
	request, err := optionalAttribute(ctx, refundRequestAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, refundOutcomeAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	return request, outcome, nil
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

// dex:group group-id:refund-request group-label:"Refund request"
// dex:explanation text:"Validate and record the refund request before calling Stripe."
type recordRefundRequest struct {
	dex.StepDefaults
}

func (recordRefundRequest) GetStepType() string { return recordRefundRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordRefundRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordRefundRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.PaymentIntentID = strings.TrimSpace(input.PaymentIntentID)
	input.Reason = strings.TrimSpace(input.Reason)
	if !strings.HasPrefix(input.PaymentIntentID, "pi_") {
		return dex.ForceFail("paymentIntentId must be a Stripe pi_ identifier"), nil
	}
	if err := refundRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](getPaymentIntentStepType), input), nil
}

// dex:group group-id:refund-planning group-label:"Refund planning"
// dex:explanation text:"Refund the unrefunded balance, or finish when nothing remains to refund."
type planRefund struct {
	dex.StepDefaultsNoWaitFor[stripe.GetPaymentIntentResult]
}

func (planRefund) GetStepType() string { return planRefundStepType }

func (planRefund) Execute(ctx dex.Context, result stripe.GetPaymentIntentResult) (*dex.StepDecision, error) {
	request, err := refundRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if RefundableAmount(result.Value) == 0 {
		outcome := Outcome{PaymentIntent: result.Value}
		if err := refundOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[RefundRequest](createRefundStepType), RefundRequest{PaymentIntent: result.Value, Reason: request.Reason}), nil
}

// dex:group group-id:refund-completion group-label:"Refund completion"
// dex:explanation text:"Persist the safe refund summary and complete the Flow."
type completeRefund struct {
	dex.StepDefaultsNoWaitFor[stripe.CreateRefundResult]
}

func (completeRefund) GetStepType() string { return completeRefundStepType }

func (completeRefund) Execute(ctx dex.Context, result stripe.CreateRefundResult) (*dex.StepDecision, error) {
	outcome := Outcome{Refund: result.Value, IsRefundCreated: true}
	if err := refundOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
