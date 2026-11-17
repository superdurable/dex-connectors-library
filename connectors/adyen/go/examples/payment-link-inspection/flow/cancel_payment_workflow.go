// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentlinkinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CancelPaymentFlowType identifies the sample payment-cancellation Flow.
const CancelPaymentFlowType = "AdyenCancelPayment"

const cancelPaymentStepType = "CallCancelPayment"

var cancellationReceiptAttribute = dex.DefineAttribute[adyen.PaymentModificationReceipt]("adyen-cancellation-receipt")

// CancelPaymentFlow requests cancellation of an uncaptured payment.
type CancelPaymentFlow struct {
	dex.FlowDefaults
	connection adyen.Connection
}

// NewCancelPaymentFlow binds the static Adyen connection.
func NewCancelPaymentFlow(connection adyen.Connection) *CancelPaymentFlow {
	return &CancelPaymentFlow{connection: connection}
}

// GetFlowType returns the stable cancellation Flow identity.
func (*CancelPaymentFlow) GetFlowType() string { return CancelPaymentFlowType }

// GetPersistenceSchema stores the asynchronous request receipt.
func (*CancelPaymentFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{cancellationReceiptAttribute}}
}

// GetSteps registers the idempotent cancellation and completion steps.
func (flow *CancelPaymentFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCancelPayment{}),
		dex.DefineStep(adyen.NewCancelPaymentStep(adyen.CancelPaymentStepConfig[adyen.PaymentReferenceModificationInput]{
			StepType: cancelPaymentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adyen", Explanation: "Request cancellation of an uncaptured payment."},
			Connection:  flow.connection,
			MapToOperationInput: func(input adyen.PaymentReferenceModificationInput) adyen.PaymentReferenceModificationInput {
				return input
			},
			Received: sdkgo.GoTo(completeCancelPayment{}),
		})),
		dex.DefineStep(completeCancelPayment{}),
	}
}

// GetRPCs exposes the receipt in Dex Web.
func (flow *CancelPaymentFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adyen-cancellation-receipt value-type:json editable:false description:"Cancellation request receipt"
// GetDexSummary shows the cancellation receipt.
func (*CancelPaymentFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	receipt, err := readCancellationReceipt(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-cancellation-receipt": receipt}}, nil
}

// dex:field attribute-key:adyen-cancellation-receipt value-type:json editable:false description:"Cancellation request receipt"
// GetDexDisplay shows the cancellation receipt.
func (*CancelPaymentFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	receipt, err := readCancellationReceipt(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-cancellation-receipt": receipt}}, nil
}

func readCancellationReceipt(ctx dex.Context) (adyen.PaymentModificationReceipt, error) {
	receipt, err := cancellationReceiptAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return adyen.PaymentModificationReceipt{}, nil
	}
	return receipt, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the merchant account and payment reference to the cancellation Step."
type startCancelPayment struct{ dex.StepDefaults }

func (startCancelPayment) GetStepType() string { return "StartCancelPayment" }
func (startCancelPayment) WaitFor(dex.Context, adyen.PaymentReferenceModificationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCancelPayment) Execute(_ dex.Context, input adyen.PaymentReferenceModificationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[adyen.PaymentReferenceModificationInput](cancelPaymentStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the asynchronous cancellation request receipt."
type completeCancelPayment struct {
	dex.StepDefaultsNoWaitFor[adyen.CancelPaymentResult]
}

func (completeCancelPayment) GetStepType() string { return "CompleteCancelPayment" }
func (completeCancelPayment) Execute(ctx dex.Context, result adyen.CancelPaymentResult) (*dex.StepDecision, error) {
	if err := cancellationReceiptAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CancelPaymentFlow)(nil)
