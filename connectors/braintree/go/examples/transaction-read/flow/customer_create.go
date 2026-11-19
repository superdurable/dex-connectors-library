// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CustomerCreateFlowType identifies the customer creation example.
const CustomerCreateFlowType = "BraintreeCustomerCreate"

const customerCreateOperationStepType = "CallCreateCustomer"

var createdCustomerAttribute = dex.DefineAttribute[braintree.CustomerProfile]("braintree-customer-created")

// CustomerCreateFlow creates one Braintree customer through GraphQL.
type CustomerCreateFlow struct {
	dex.FlowDefaults
	connection braintree.Connection
}

// NewCustomerCreateFlow binds the provider connection at registration time.
func NewCustomerCreateFlow(connection braintree.Connection) *CustomerCreateFlow {
	return &CustomerCreateFlow{connection: connection}
}

func (*CustomerCreateFlow) GetFlowType() string { return CustomerCreateFlowType }

func (*CustomerCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdCustomerAttribute}}
}

func (flow *CustomerCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerCreateRequest{}),
		dex.DefineStep(braintree.NewCreateCustomerStep(braintree.CreateCustomerStepConfig[braintree.CreateCustomerInput]{
			StepType: customerCreateOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braintree", Explanation: "Create one customer from contact details."},
			Connection:          flow.connection,
			MapToOperationInput: func(input braintree.CreateCustomerInput) braintree.CreateCustomerInput { return input },
			Created:             sdkgo.GoTo(completeCustomerCreateResult{}),
		})),
		dex.DefineStep(completeCustomerCreateResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CustomerCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:braintree-customer-created value-type:json editable:false description:"Provider result"
// GetDexSummary shows the created customer.
func (*CustomerCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	customer, err := readCreatedCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-customer-created": customer}}, nil
}

// dex:field attribute-key:braintree-customer-created value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the created customer.
func (*CustomerCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	customer, err := readCreatedCustomer(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braintree-customer-created": customer}}, nil
}

func readCreatedCustomer(ctx dex.Context) (braintree.CustomerProfile, error) {
	customer, err := createdCustomerAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braintree.CustomerProfile{}, nil
	}
	return customer, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass contact details to the provider Step."
type startCustomerCreateRequest struct{ dex.StepDefaults }

func (startCustomerCreateRequest) GetStepType() string { return "StartCreateCustomer" }
func (startCustomerCreateRequest) WaitFor(dex.Context, braintree.CreateCustomerInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerCreateRequest) Execute(_ dex.Context, input braintree.CreateCustomerInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braintree.CreateCustomerInput](customerCreateOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the customer result."
type completeCustomerCreateResult struct {
	dex.StepDefaultsNoWaitFor[braintree.CreateCustomerResult]
}

func (completeCustomerCreateResult) GetStepType() string { return "CompleteCreateCustomer" }
func (completeCustomerCreateResult) Execute(ctx dex.Context, result braintree.CreateCustomerResult) (*dex.StepDecision, error) {
	if err := createdCustomerAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerCreateFlow)(nil)
