// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package resourceread demonstrates a read-only Square connector Flow.
package resourceread

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/square/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "SquareResourceRead"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName         = "square"
	readStepType           = "ReadSquareResource"
	createPaymentStepType  = "CreateSquareCashPayment"
	refundPaymentStepType  = "RefundSquarePayment"
	createCustomerStepType = "CreateSquareCustomer"
)

// Request is the typed start input for this example.
type Request struct {
	// Cursor selects the resource for the read.
	Cursor string `json:"cursor"`
	// RecordCashPayment selects the payment write instead of the payment list.
	RecordCashPayment bool `json:"recordCashPayment,omitempty"`
	// AmountMoney is the positive cash payment amount in minor currency units when recording a payment.
	AmountMoney square.Money `json:"amountMoney,omitempty"`
	// RefundPaymentID selects a completed payment to refund when set.
	RefundPaymentID string `json:"refundPaymentId,omitempty"`
	// CreateCustomer selects the customer create path when true.
	CreateCustomer bool `json:"createCustomer,omitempty"`
	// CustomerGivenName is the new customer's given name.
	CustomerGivenName string `json:"customerGivenName,omitempty"`
	// CustomerEmailAddress is the new customer's email address.
	CustomerEmailAddress string `json:"customerEmailAddress,omitempty"`
}

// Outcome is the completed resource read.
type Outcome struct {
	// Value holds the provider resource or page.
	Value square.PaymentPage `json:"value"`
	// Payment is the created cash payment when RecordCashPayment was selected.
	Payment square.Payment `json:"payment,omitempty"`
	// Refund is the accepted payment refund when RefundPaymentID was set.
	Refund square.Refund `json:"refund,omitempty"`
	// Customer is the new Square customer when CreateCustomer was selected.
	Customer square.Customer `json:"customer,omitempty"`
}

var outcomeAttribute = dex.DefineAttribute[Outcome]("square-resource-read-outcome")

// Flow reads one Square resource from the declared connection.
type Flow struct {
	dex.FlowDefaults
	connection square.Connection
}

// NewFlow binds a trusted connection at registration.
func NewFlow(connection square.Connection) *Flow { return &Flow{connection: connection} }
func (*Flow) GetFlowType() string                { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRequest{}),
		dex.DefineStep(square.NewListPaymentsStep(square.ListPaymentsStepConfig[Request]{
			StepType: readStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "square", GroupLabel: "Square", Explanation: "Read the requested Square resource."},
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeRead{}),
		})),
		dex.DefineStep(square.NewCreatePaymentStep(square.CreatePaymentStepConfig[Request]{
			StepType: createPaymentStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "square", GroupLabel: "Square", Explanation: "Record the requested cash payment."},
			MapToOperationInput: MapToCreatePaymentInput,
			Created:             sdkgo.GoTo(completeCashPayment{}),
		})),
		dex.DefineStep(square.NewRefundPaymentStep(square.RefundPaymentStepConfig[Request]{
			StepType: refundPaymentStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "square", GroupLabel: "Square", Explanation: "Refund the selected Square payment."},
			MapToOperationInput: MapToRefundPaymentInput,
			Refunded:            sdkgo.GoTo(completeRefund{}),
		})),
		dex.DefineStep(square.NewCreateCustomerStep(square.CreateCustomerStepConfig[Request]{
			StepType: createCustomerStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "square", GroupLabel: "Square", Explanation: "Create the requested customer profile."},
			MapToOperationInput: MapToCreateCustomerInput,
			Created:             sdkgo.GoTo(completeCustomer{}),
		})),
		dex.DefineStep(completeRead{}),
		dex.DefineStep(completeCashPayment{}),
		dex.DefineStep(completeRefund{}),
		dex.DefineStep(completeCustomer{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:square-resource-read-outcome value-type:json editable:false description:"Resource read outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"square-resource-read-outcome": outcome}}, nil
}

// dex:field attribute-key:square-resource-read-outcome value-type:json editable:false description:"Resource read outcome"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"square-resource-read-outcome": outcome}}, nil
}

// MapToOperationInput maps the typed request into one provider read.
func MapToOperationInput(request Request) square.ListPaymentsInput {
	return square.ListPaymentsInput{Cursor: request.Cursor}
}

// MapToCreatePaymentInput maps a cash request without storing a payment token in the Flow.
func MapToCreatePaymentInput(request Request) square.CreatePaymentInput {
	return square.CreatePaymentInput{SourceID: "CASH", AmountMoney: request.AmountMoney}
}

// MapToRefundPaymentInput maps a completed payment and refund amount.
func MapToRefundPaymentInput(request Request) square.RefundPaymentInput {
	return square.RefundPaymentInput{PaymentID: request.RefundPaymentID, AmountMoney: request.AmountMoney}
}

// MapToCreateCustomerInput maps the customer profile fields.
func MapToCreateCustomerInput(request Request) square.CreateCustomerInput {
	return square.CreateCustomerInput{GivenName: request.CustomerGivenName, EmailAddress: request.CustomerEmailAddress}
}

// PrepareRequest rejects invalid local input before a provider call.
func PrepareRequest(request Request) (Request, error) {
	if request.RecordCashPayment && (request.AmountMoney.Amount <= 0 || request.AmountMoney.Currency == "") {
		return Request{}, errors.New("cash payment amount and currency are required")
	}
	if request.RefundPaymentID != "" && (request.AmountMoney.Amount <= 0 || request.AmountMoney.Currency == "") {
		return Request{}, errors.New("refund amount and currency are required")
	}
	if request.RecordCashPayment && request.RefundPaymentID != "" {
		return Request{}, errors.New("choose one payment action")
	}
	if request.CreateCustomer {
		if request.CustomerGivenName == "" && request.CustomerEmailAddress == "" {
			return Request{}, errors.New("customer given name or email address is required")
		}
		if request.RecordCashPayment || request.RefundPaymentID != "" {
			return Request{}, errors.New("choose one Square action")
		}
	}
	return request, nil
}

// dex:group group-id:resource-read group-label:"Resource read"
// dex:explanation text:"Validate the request before reading the provider."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareResourceRead" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(ctx dex.Context, request Request) (*dex.StepDecision, error) {
	prepared, err := PrepareRequest(request)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if prepared.RecordCashPayment {
		return dex.GoTo(sdkgo.StepRef[Request](createPaymentStepType), prepared), nil
	}
	if prepared.RefundPaymentID != "" {
		return dex.GoTo(sdkgo.StepRef[Request](refundPaymentStepType), prepared), nil
	}
	if prepared.CreateCustomer {
		return dex.GoTo(sdkgo.StepRef[Request](createCustomerStepType), prepared), nil
	}
	return dex.GoTo(sdkgo.StepRef[Request](readStepType), prepared), nil
}

// dex:group group-id:resource-result group-label:"Resource result"
// dex:explanation text:"Persist and return the provider result as the Flow outcome."
type completeRead struct {
	dex.StepDefaultsNoWaitFor[square.ListPaymentsResult]
}

func (completeRead) GetStepType() string { return "CompleteResourceRead" }
func (completeRead) Execute(ctx dex.Context, result square.ListPaymentsResult) (*dex.StepDecision, error) {
	outcome := Outcome{Value: result.Value}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:resource-result group-label:"Resource result"
// dex:explanation text:"Persist the created cash payment as the Flow outcome."
type completeCashPayment struct {
	dex.StepDefaultsNoWaitFor[square.CreatePaymentResult]
}

func (completeCashPayment) GetStepType() string { return "CompleteCashPayment" }
func (completeCashPayment) Execute(ctx dex.Context, result square.CreatePaymentResult) (*dex.StepDecision, error) {
	outcome := Outcome{Payment: result.Value}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:resource-result group-label:"Resource result"
// dex:explanation text:"Persist the accepted refund as the Flow outcome."
type completeRefund struct {
	dex.StepDefaultsNoWaitFor[square.RefundPaymentResult]
}

func (completeRefund) GetStepType() string { return "CompleteRefund" }
func (completeRefund) Execute(ctx dex.Context, result square.RefundPaymentResult) (*dex.StepDecision, error) {
	outcome := Outcome{Refund: result.Value}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:resource-result group-label:"Resource result"
// dex:explanation text:"Persist the created customer as the Flow outcome."
type completeCustomer struct {
	dex.StepDefaultsNoWaitFor[square.CreateCustomerResult]
}

func (completeCustomer) GetStepType() string { return "CompleteCustomer" }
func (completeCustomer) Execute(ctx dex.Context, result square.CreateCustomerResult) (*dex.StepDecision, error) {
	outcome := Outcome{Customer: result.Value}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
