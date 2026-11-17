// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CustomerFlowType identifies the customer read example.
const CustomerFlowType = "BraintreeCustomerRead"

const customerOperationStepType = "CallGetCustomer"

var customerAttribute = dex.DefineAttribute[braintree.CustomerProfile]("braintree-customer-read")

// CustomerFlow reads one Braintree customer through the GraphQL node query.
type CustomerFlow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewCustomerFlow binds the provider connection at registration time.
func NewCustomerFlow(connection braintree.Connection) *CustomerFlow {
	return &CustomerFlow{connection: connection}
}

func (*CustomerFlow) GetFlowType() string { return CustomerFlowType }

func (*CustomerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerAttribute}}
}

func (flow *CustomerFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerRequest{}),
		dex.DefineStep(braintree.NewGetCustomerStep(braintree.GetCustomerStepConfig[braintree.GetCustomerInput]{
			StepType: customerOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Find one customer by GraphQL ID."},
			Connection:          flow.connection,
			MapToOperationInput: func(input braintree.GetCustomerInput) braintree.GetCustomerInput { return input },
			Found:               sdkgo.GoTo(completeCustomerResult{}),
		})),
		dex.DefineStep(completeCustomerResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CustomerFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-customer-read value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last customer result.
func (*CustomerFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	customer, err := readCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-customer-read": customer}}, nil
}

// dex:field attribute-key:braintree-customer-read value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last customer result.
func (*CustomerFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	customer, err := readCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-customer-read": customer}}, nil
}

func readCustomer(ctx dex.Context) (braintree.CustomerProfile, error) {
	customer, err := customerAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.CustomerProfile{}, nil
	}
	return customer, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the customer ID to the provider Step."
type startCustomerRequest struct{ dex.StepDefaults }

func (startCustomerRequest) GetStepType() string { return "StartGetCustomer" }
func (startCustomerRequest) WaitFor(dex.Context, braintree.GetCustomerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerRequest) Execute(_ dex.Context, input braintree.GetCustomerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.GetCustomerInput](customerOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the customer result."
type completeCustomerResult struct {
	dex.StepDefaultsNoWaitFor[braintree.GetCustomerResult]
}

func (completeCustomerResult) GetStepType() string { return "CompleteGetCustomer" }
func (completeCustomerResult) Execute(ctx dex.Context, result braintree.GetCustomerResult) (*dex.StepDecision, error) {
	if err := customerAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerFlow)(nil)
