// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TransactionChargeFlowType identifies the transaction creation example.
const TransactionChargeFlowType = "BraintreeTransactionCharge"

const transactionChargeOperationStepType = "CallChargePaymentMethod"

var chargedTransactionAttribute = dex.DefineAttribute[braintree.TransactionProfile]("braintree-transaction-charged")

// TransactionChargeFlow creates one Braintree transaction through GraphQL.
type TransactionChargeFlow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewTransactionChargeFlow binds the provider connection at registration time.
func NewTransactionChargeFlow(connection braintree.Connection) *TransactionChargeFlow {
	return &TransactionChargeFlow{connection: connection}
}

func (*TransactionChargeFlow) GetFlowType() string { return TransactionChargeFlowType }

func (*TransactionChargeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{chargedTransactionAttribute}}
}

func (flow *TransactionChargeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTransactionChargeRequest{}),
		dex.DefineStep(braintree.NewChargePaymentMethodStep(braintree.ChargePaymentMethodStepConfig[braintree.ChargePaymentMethodInput]{
			StepType: transactionChargeOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Charge one tokenized payment method with a stable request key."},
			Connection:          flow.connection,
			MapToOperationInput: func(input braintree.ChargePaymentMethodInput) braintree.ChargePaymentMethodInput { return input },
			Charged:             sdkgo.GoTo(completeTransactionChargeResult{}),
		})),
		dex.DefineStep(completeTransactionChargeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *TransactionChargeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-transaction-charged value-type:json editable:false description:"Provider result"
// GetDexSummary shows the charged transaction.
func (*TransactionChargeFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	transaction, err := readChargedTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-transaction-charged": transaction}}, nil
}

// dex:field attribute-key:braintree-transaction-charged value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the charged transaction.
func (*TransactionChargeFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	transaction, err := readChargedTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-transaction-charged": transaction}}, nil
}

func readChargedTransaction(ctx dex.Context) (braintree.TransactionProfile, error) {
	transaction, err := chargedTransactionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.TransactionProfile{}, nil
	}
	return transaction, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the payment method and amount to the provider Step."
type startTransactionChargeRequest struct{ dex.StepDefaults }

func (startTransactionChargeRequest) GetStepType() string { return "StartChargePaymentMethod" }
func (startTransactionChargeRequest) WaitFor(dex.Context, braintree.ChargePaymentMethodInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTransactionChargeRequest) Execute(_ dex.Context, input braintree.ChargePaymentMethodInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.ChargePaymentMethodInput](transactionChargeOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the transaction result."
type completeTransactionChargeResult struct {
	dex.StepDefaultsNoWaitFor[braintree.ChargePaymentMethodResult]
}

func (completeTransactionChargeResult) GetStepType() string { return "CompleteChargePaymentMethod" }
func (completeTransactionChargeResult) Execute(ctx dex.Context, result braintree.ChargePaymentMethodResult) (*dex.StepDecision, error) {
	if err := chargedTransactionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TransactionChargeFlow)(nil)
