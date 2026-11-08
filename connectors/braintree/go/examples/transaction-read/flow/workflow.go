// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package transactionread runs Braintree getTransaction as one Dex Flow.
package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BraintreeTransactionRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "braintree"
	operationStepType = "CallGetTransaction"
)

var transactionAttribute = dex.DefineAttribute[braintree.TransactionProfile]("braintree-transaction-read")

// Flow runs GetTransaction once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection braintree.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{transactionAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(braintree.NewGetTransactionStep(braintree.GetTransactionStepConfig[braintree.GetTransactionInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Find one transaction by ID in Braintree."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-transaction-read value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-transaction-read": outcome}}, nil
}

// dex:field attribute-key:braintree-transaction-read value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-transaction-read": outcome}}, nil
}

func readTransaction(ctx dex.Context) (braintree.TransactionProfile, error) {
	outcome, err := transactionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.TransactionProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input braintree.GetTransactionInput) braintree.GetTransactionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTransaction" }
func (startRequest) WaitFor(dex.Context, braintree.GetTransactionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input braintree.GetTransactionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.GetTransactionInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[braintree.GetTransactionResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTransaction" }
func (completeResult) Execute(ctx dex.Context, result braintree.GetTransactionResult) (*dex.StepDecision, error) {
	if err := transactionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
