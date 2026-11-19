// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/razorpay/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createRefundFlowType = "RazorpayCreateNormalRefund"
const createRefundStepType = "CallCreateNormalRefund"

var refundAttribute = dex.DefineAttribute[razorpay.RefundStatus]("razorpay-created-refund")

// CreateRefundFlow issues one idempotent normal refund and records its safe status.
type CreateRefundFlow struct {
	dex.FlowDefaults
	connection razorpay.Connection
}

// NewCreateRefundFlow binds the static Razorpay connection.
func NewCreateRefundFlow(connection razorpay.Connection) *CreateRefundFlow {
	return &CreateRefundFlow{connection: connection}
}

func (*CreateRefundFlow) GetFlowType() string { return createRefundFlowType }

func (*CreateRefundFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{refundAttribute}}
}

func (flow *CreateRefundFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateRefund{}),
		dex.DefineStep(razorpay.NewCreateNormalRefundStep(razorpay.CreateNormalRefundStepConfig[razorpay.CreateNormalRefundInput]{
			StepType: createRefundStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Razorpay", Explanation: "Issue one idempotent normal refund."},
			Connection:          flow.connection,
			MapToOperationInput: mapCreateRefundInput,
			Created:             sdkgo.GoTo(completeCreateRefund{}),
		})),
		dex.DefineStep(completeCreateRefund{}),
	}
}

func (flow *CreateRefundFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:razorpay-created-refund value-type:json editable:false description:"Refund status"
// GetDexSummary returns the safe refund summary for Dex Web.
func (*CreateRefundFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedRefund(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"razorpay-created-refund": value}}, nil
}

// dex:field attribute-key:razorpay-created-refund value-type:json editable:false description:"Refund status"
// GetDexDisplay returns the safe refund summary for Dex Web.
func (*CreateRefundFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedRefund(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"razorpay-created-refund": value}}, nil
}

func readCreatedRefund(ctx dex.Context) (razorpay.RefundStatus, error) {
	value, err := refundAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return razorpay.RefundStatus{}, nil
	}
	return value, err
}

func mapCreateRefundInput(input razorpay.CreateNormalRefundInput) razorpay.CreateNormalRefundInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the payment and amount to the refund Step."
type startCreateRefund struct{ dex.StepDefaults }

func (startCreateRefund) GetStepType() string { return "StartCreateNormalRefund" }
func (startCreateRefund) WaitFor(dex.Context, razorpay.CreateNormalRefundInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateRefund) Execute(_ dex.Context, input razorpay.CreateNormalRefundInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[razorpay.CreateNormalRefundInput](createRefundStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Razorpay accepts the refund."
type completeCreateRefund struct {
	dex.StepDefaultsNoWaitFor[razorpay.CreateNormalRefundResult]
}

func (completeCreateRefund) GetStepType() string { return "CompleteCreateNormalRefund" }
func (completeCreateRefund) Execute(ctx dex.Context, result razorpay.CreateNormalRefundResult) (*dex.StepDecision, error) {
	if err := refundAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateRefundFlow)(nil)
