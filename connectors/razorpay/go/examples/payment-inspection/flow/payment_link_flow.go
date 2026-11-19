// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/razorpay/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createPaymentLinkFlowType = "RazorpayCreatePaymentLink"
const createPaymentLinkStepType = "CallCreatePaymentLink"

var paymentLinkAttribute = dex.DefineAttribute[razorpay.PaymentLinkSummary]("razorpay-created-payment-link")

// CreatePaymentLinkFlow creates one payment link with a call-specific reference.
type CreatePaymentLinkFlow struct {
	dex.FlowDefaults
	connection razorpay.Connection
}

// NewCreatePaymentLinkFlow binds the static Razorpay connection.
func NewCreatePaymentLinkFlow(connection razorpay.Connection) *CreatePaymentLinkFlow {
	return &CreatePaymentLinkFlow{connection: connection}
}

func (*CreatePaymentLinkFlow) GetFlowType() string { return createPaymentLinkFlowType }

func (*CreatePaymentLinkFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{paymentLinkAttribute}}
}

func (flow *CreatePaymentLinkFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreatePaymentLink{}),
		dex.DefineStep(razorpay.NewCreatePaymentLinkStep(razorpay.CreatePaymentLinkStepConfig[razorpay.CreatePaymentLinkInput]{
			StepType: createPaymentLinkStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Razorpay", Explanation: "Create one payment link without notifications."},
			Connection:          flow.connection,
			MapToOperationInput: mapCreatePaymentLinkInput,
			Created:             sdkgo.GoTo(completeCreatePaymentLink{}),
		})),
		dex.DefineStep(completeCreatePaymentLink{}),
	}
}

func (flow *CreatePaymentLinkFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:razorpay-created-payment-link value-type:json editable:false description:"Created payment link"
// GetDexSummary returns the safe payment link summary for Dex Web.
func (*CreatePaymentLinkFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"razorpay-created-payment-link": value}}, nil
}

// dex:field attribute-key:razorpay-created-payment-link value-type:json editable:false description:"Created payment link"
// GetDexDisplay returns the safe payment link summary for Dex Web.
func (*CreatePaymentLinkFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedPaymentLink(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"razorpay-created-payment-link": value}}, nil
}

func readCreatedPaymentLink(ctx dex.Context) (razorpay.PaymentLinkSummary, error) {
	value, err := paymentLinkAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return razorpay.PaymentLinkSummary{}, nil
	}
	return value, err
}

func mapCreatePaymentLinkInput(input razorpay.CreatePaymentLinkInput) razorpay.CreatePaymentLinkInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass amount and currency to the payment link Step."
type startCreatePaymentLink struct{ dex.StepDefaults }

func (startCreatePaymentLink) GetStepType() string { return "StartCreatePaymentLink" }
func (startCreatePaymentLink) WaitFor(dex.Context, razorpay.CreatePaymentLinkInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreatePaymentLink) Execute(_ dex.Context, input razorpay.CreatePaymentLinkInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[razorpay.CreatePaymentLinkInput](createPaymentLinkStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Razorpay confirms the payment link."
type completeCreatePaymentLink struct {
	dex.StepDefaultsNoWaitFor[razorpay.CreatePaymentLinkResult]
}

func (completeCreatePaymentLink) GetStepType() string { return "CompleteCreatePaymentLink" }
func (completeCreatePaymentLink) Execute(ctx dex.Context, result razorpay.CreatePaymentLinkResult) (*dex.StepDecision, error) {
	if err := paymentLinkAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreatePaymentLinkFlow)(nil)
